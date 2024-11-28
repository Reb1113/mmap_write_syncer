package logger

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	backupTimeFormat    = "2006-01-02T15-04-05.000"
	compressSuffix      = ".gz"
	defaultMmapMaxSize  = 100
	defaultMegaByteSize = 10 //每次mmap映射size
)

var _ io.WriteCloser = (*MMapLogger)(nil)

type MMapLogger struct {
	Filename   string `json:"filename" yaml:"filename"`     // 指定日志文件的名称。如果不提供，则默认使用<processname>-mmap.log并保存在os.TempDir()目录下。
	MaxSize    int    `json:"maxsize" yaml:"maxsize"`       // 指定日志文件的最大大小（以兆字节为单位）。当日志文件达到此大小时，将触发轮换。默认值为100兆字节。
	MaxAge     int    `json:"maxage" yaml:"maxage"`         // 基于日志文件名中编码的时间戳，指定保留旧日志文件的最大天数
	MaxBackups int    `json:"maxbackups" yaml:"maxbackups"` // 指定要保留的旧日志文件的最大数量
	LocalTime  bool   `json:"localtime" yaml:"localtime"`   // 确定用于格式化备份文件中的时间戳的时间是否为计算机的本地时间
	Compress   bool   `json:"compress" yaml:"compress"`     // 确定是否应使用gzip压缩旋转的日志文件。默认情况下，不执行压缩。

	size      int64      // 当前日志文件的大小
	file      *os.File   // 当前打开的日志文件
	mu        sync.Mutex // 用于保护对当前日志文件的并发访问的互斥锁
	millCh    chan bool  // 用于通知日志文件即将旋转的通道
	startMill sync.Once  // 确保日志轮换监控只启动一次的单例

	writeStartAt int64  // 当前mmap映射write开始位置
	writeAt      int64  // 当前映射write的位置
	mmapSpace    []byte // 文件和内存的映射空间
}

var (
	currentTime = time.Now
	os_Stat     = os.Stat
	megabyte    = 1024 * 1024
	pageSize    = 4 * 1024
)

// 停止 MMapLogger
func (l *MMapLogger) StopMmapLogger() {
	if l != nil {
		l.unMap()      // 解除内存映射
		l.file.Close() // 关闭文件
	}
}

// Write 向 MMapLogger 写入数据
func (l *MMapLogger) Write(p []byte) (n int, err error) {
	l.mu.Lock()               // 加锁
	defer l.mu.Unlock()       // 解锁
	writeLen := int64(len(p)) // 写入数据长度
	if writeLen > l.max() {   // 如果写入长度超过最大限制
		return 0, fmt.Errorf("write length %d exceeds maximum file size %d", writeLen, l.max())
	}
	if l.file == nil { // 如果文件未打开
		if err = l.openExistingOrNew(); err != nil { // 尝试打开现有文件或创建新文件
			return 0, err
		}
	}
	if len(p) >= int(l.size)-int(l.writeAt) { // 如果写入数据会导致文件超过最大大小
		if err := l.allocateSpace(); err != nil { // 尝试分配更多空间
			fmt.Printf("allocateSpace fail. error: %+v", err)
			return len(p), err
		}
	}
	cacheAt := l.writeAt - l.writeStartAt       // 计算缓存位置
	if len(p)+int(cacheAt) > len(l.mmapSpace) { // 如果写入数据会导致内存映射空间不足
		return len(p), err
	}
	copy(l.mmapSpace[cacheAt:], p) // 将数据复制到内存映射空间
	l.writeAt += int64(len(p))     // 更新写入位置
	l.size += int64(n)             // 更新文件大小
	return n, err
}

// 关闭 MMapLogger 实例的文件，并释放相关资源。
func (l *MMapLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.close()
}

func (l *MMapLogger) close() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// 旋转日志文件，创建一个新的日志文件并关闭旧的日志文件
func (l *MMapLogger) Rotate() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rotate()
}

// 执行日志文件的旋转操作
func (l *MMapLogger) rotate() error {
	if err := l.close(); err != nil {
		return err
	}
	if err := l.openNew(); err != nil {
		return err
	}
	l.mill()
	return nil
}

// 创建一个新的日志文件
func (l *MMapLogger) openNew() error {
	err := os.MkdirAll(l.dir(), 0664)
	if err != nil {
		return fmt.Errorf("can't make directories for new logfile: %s", err)
	}

	name := l.filename()
	info, err := os_Stat(name)
	if err == nil {
		newname := backupName(name, l.LocalTime)
		if err := os.Rename(name, newname); err != nil {
			return fmt.Errorf("can't rename log file: %s", err)
		}

		if err := chown(name, info); err != nil {
			return err
		}
	}

	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0664)
	if err != nil {
		return fmt.Errorf("can't open new logfile: %s", err)
	}
	l.file = f
	fileStat, err := l.file.Stat()
	if err != nil {
		fmt.Printf("获取文件信息错误：%+v\n", err)
		return err
	}
	l.size = fileStat.Size()
	l.writeAt = fileStat.Size()
	return nil
}

// 生成备份文件名
func backupName(name string, local bool) string {
	dir := filepath.Dir(name)
	filename := filepath.Base(name)
	ext := filepath.Ext(filename)
	prefix := filename[:len(filename)-len(ext)]
	t := currentTime()
	if !local {
		t = t.UTC()
	}

	timestamp := t.Format(backupTimeFormat)
	return filepath.Join(dir, fmt.Sprintf("%s-%s%s", prefix, timestamp, ext))
}

// 打开现有的日志文件或创建一个新的日志文件
func (l *MMapLogger) openExistingOrNew() error {
	l.mill()
	filename := l.filename()
	_, err := os_Stat(filename)
	if os.IsNotExist(err) {
		return l.openNew()
	}
	if err != nil {
		return fmt.Errorf("error getting log file info: %s", err)
	}

	file, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0664)
	if err != nil {
		return l.openNew()
	}
	fileStat, err := file.Stat()
	if err != nil {
		fmt.Printf("获取文件信息错误：%+v\n", err)
		return err
	}
	l.file = file
	l.size = fileStat.Size()
	l.writeAt = fileStat.Size()
	return nil
}

func (l *MMapLogger) filename() string {
	if l.Filename != "" {
		return l.Filename
	}
	name := filepath.Base(os.Args[0]) + "-mmap.log"
	return filepath.Join(os.TempDir(), name)
}

// 启动日志文件轮换的协程
func (l *MMapLogger) mill() {
	l.startMill.Do(func() {
		l.millCh = make(chan bool, 1)
		go l.millRun()
	})
	select {
	case l.millCh <- true:
	default:
	}
}

// 运行日志文件轮换的协程
func (l *MMapLogger) millRun() {
	for _ = range l.millCh {
		_ = l.millRunOnce()
	}
}

// 执行一次日志文件轮换操作
func (l *MMapLogger) millRunOnce() error {
	if l.MaxBackups == 0 && l.MaxAge == 0 && !l.Compress {
		return nil
	}

	files, err := l.oldLogFiles()
	if err != nil {
		return err
	}

	var compress, remove []logInfo

	if l.MaxBackups > 0 && l.MaxBackups < len(files) {
		preserved := make(map[string]bool)
		var remaining []logInfo
		for _, f := range files {
			fn := f.Name()
			if strings.HasSuffix(fn, compressSuffix) {
				fn = fn[:len(fn)-len(compressSuffix)]
			}
			preserved[fn] = true

			if len(preserved) > l.MaxBackups {
				remove = append(remove, f)
			} else {
				remaining = append(remaining, f)
			}
		}
		files = remaining
	}
	if l.MaxAge > 0 {
		diff := time.Duration(int64(24*time.Hour) * int64(l.MaxAge))
		cutoff := currentTime().Add(-1 * diff)

		var remaining []logInfo
		for _, f := range files {
			if f.timestamp.Before(cutoff) {
				remove = append(remove, f)
			} else {
				remaining = append(remaining, f)
			}
		}
		files = remaining
	}

	if l.Compress {
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), compressSuffix) {
				compress = append(compress, f)
			}
		}
	}

	for _, f := range remove {
		errRemove := os.Remove(filepath.Join(l.dir(), f.Name()))
		if err == nil && errRemove != nil {
			err = errRemove
		}
	}
	for _, f := range compress {
		fn := filepath.Join(l.dir(), f.Name())
		errCompress := compressLogFile(fn, fn+compressSuffix)
		if err == nil && errCompress != nil {
			err = errCompress
		}
	}

	return err
}

// 压缩指定的日志文件，并将其重命名为指定的目标文件名
func compressLogFile(src, dst string) (err error) {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}
	defer f.Close()

	fi, err := os_Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat log file: %v", err)
	}

	if err := chown(dst, fi); err != nil {
		return fmt.Errorf("failed to chown compressed log file: %v", err)
	}

	gzf, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE, 0664)
	if err != nil {
		return fmt.Errorf("failed to open compressed log file: %v", err)
	}
	defer gzf.Close()

	gz := gzip.NewWriter(gzf)

	defer func() {
		if err != nil {
			os.Remove(dst)
			err = fmt.Errorf("failed to compress log file: %v", err)
		}
	}()

	if _, err := io.Copy(gz, f); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := gzf.Close(); err != nil {
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		return err
	}

	return nil
}

// 获取日志文件目录中的所有旧日志文件信息
func (l *MMapLogger) oldLogFiles() ([]logInfo, error) {
	files, err := ioutil.ReadDir(l.dir())
	if err != nil {
		return nil, fmt.Errorf("can't read log file directory: %s", err)
	}
	logFiles := []logInfo{}

	prefix, ext := l.prefixAndExt()

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if t, err := l.timeFromName(f.Name(), prefix, ext); err == nil {
			logFiles = append(logFiles, logInfo{t, f})
			continue
		}
		if t, err := l.timeFromName(f.Name(), prefix, ext+compressSuffix); err == nil {
			logFiles = append(logFiles, logInfo{t, f})
			continue
		}
	}

	sort.Sort(byFormatTime(logFiles))

	return logFiles, nil
}

// 从文件名中解析出时间戳
func (l *MMapLogger) timeFromName(filename, prefix, ext string) (time.Time, error) {
	if !strings.HasPrefix(filename, prefix) {
		return time.Time{}, errors.New("mismatched prefix")
	}
	if !strings.HasSuffix(filename, ext) {
		return time.Time{}, errors.New("mismatched extension")
	}
	ts := filename[len(prefix) : len(filename)-len(ext)]
	return time.Parse(backupTimeFormat, ts)
}

// 返回最大文件大小。
func (l *MMapLogger) max() int64 {
	if l.MaxSize == 0 {
		return int64(defaultMmapMaxSize * megabyte)
	}
	return int64(l.MaxSize) * int64(megabyte)
}

// 返回文件所在目录
func (l *MMapLogger) dir() string {
	return filepath.Dir(l.filename())
}

// 返回文件的前缀和扩展名
func (l *MMapLogger) prefixAndExt() (prefix, ext string) {
	filename := filepath.Base(l.filename())
	ext = filepath.Ext(filename)
	prefix = filename[:len(filename)-len(ext)] + "-"
	return prefix, ext
}

type logInfo struct {
	timestamp time.Time
	os.FileInfo
}

type byFormatTime []logInfo

func (b byFormatTime) Less(i, j int) bool {
	return b[i].timestamp.After(b[j].timestamp)
}

func (b byFormatTime) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func (b byFormatTime) Len() int {
	return len(b)
}

var os_Chown = os.Chown

// 改变指定文件的所有者和组
func chown(name string, info os.FileInfo) error {
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0664)
	if err != nil {
		return err
	}
	f.Close()
	stat := info.Sys().(*syscall.Stat_t)
	return os_Chown(name, int(stat.Uid), int(stat.Gid))
}

// 解析内存映射文件
func (l *MMapLogger) unMap() error {
	// 如果内存映射空间为空，则直接返回，无需解映射
	if len(l.mmapSpace) == 0 {
		return nil
	}
	// 使用 syscall.Munmap 函数解映射内存映射空间
	if err := syscall.Munmap(l.mmapSpace); err != nil {
		return err
	}
	// 使用 syscall.Ftruncate 函数调整文件大小至写入位置
	if err := syscall.Ftruncate(int(l.file.Fd()), l.writeAt); err != nil {
		// 如果调整文件大小失败，则打印错误信息
		fmt.Printf("unMap Ftruncate file fail. error: %v", err)
	}
	// 返回 nil 表示解映射和调整文件大小成功
	return nil
}

// 分配内存映射空间
func (l *MMapLogger) allocateSpace() error {
	// 先解除当前的内存映射
	if err := l.unMap(); err != nil {
		// 如果解除映射失败，则打印错误信息并返回错误
		fmt.Printf("unMap fail. error: %v", err)
		return err
	}
	// 计算新的内存映射空间的大小（默认大小乘以兆字节）
	megaByteSize := defaultMegaByteSize * megabyte
	// 计算当前写入位置对应的页数
	pageLen := int64(l.writeAt / int64(pageSize))
	// 计算新的写入起始位置
	writeStartAt := int64(pageLen * int64(pageSize))
	// 如果新的写入起始位置加上新的内存映射空间大小超过最大限制，则尝试旋转日志文件
	if writeStartAt+int64(megaByteSize) > l.max() {
		if err := l.rotate(); err != nil {
			// 如果旋转日志文件失败，则打印错误信息并返回错误
			fmt.Printf("rotate fail. error: %v", err)
			return err
		}
		// 重置页数和写入起始位置
		pageLen = 0
		writeStartAt = 0
	}
	// 调整文件大小以适应新的内存映射空间
	if err := syscall.Ftruncate(int(l.file.Fd()), writeStartAt+int64(megaByteSize)); err != nil {
		// 如果调整文件大小失败，则打印错误信息并返回错误
		fmt.Printf("syscall Ftruncate fail. error: %v", err)
		return err
	}
	// 创建新的内存映射空间
	mmapSpace, err := syscall.Mmap(int(l.file.Fd()), writeStartAt, int(megaByteSize), syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		// 如果创建内存映射空间失败，则打印错误信息并返回错误
		fmt.Printf("syscall mmap fail.  error: %v", err)
		return err
	}
	// 更新 MMapLogger 的相关字段
	l.mmapSpace = mmapSpace
	l.writeStartAt = writeStartAt
	l.size = writeStartAt + int64(megaByteSize)
	return nil
}
