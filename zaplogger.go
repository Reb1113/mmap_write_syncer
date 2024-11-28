package log

import (
	"os"

	"github.com/Reb1113/mmap_write_syncer/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var mmapLogger *logger.MMapLogger

// New returns a Logger instance.
func New(config *Config) Logger {
	if config == nil {
		config = defaultConfig
	}

	var encoder zapcore.Encoder
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "time"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	if config.DevMode {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	if config.Filename == "" {
		config.Filename = defaultFilename
	}
	if config.MaxSize <= 0 {
		config.MaxSize = defaultMaxSize
	}
	if config.MaxAge <= 0 {
		config.MaxAge = defaultMaxAge
	}
	if config.MaxBackups <= 0 {
		config.MaxBackups = defaultMaxBackups
	}
	lumberJackLogger := &lumberjack.Logger{
		Filename:   config.Filename,
		MaxSize:    config.MaxSize,
		MaxAge:     config.MaxAge,
		MaxBackups: config.MaxBackups,
		LocalTime:  true,
		Compress:   config.Compress,
	}
	mmapLogger = &logger.MMapLogger{
		Filename:   config.Filename,
		MaxSize:    config.MaxSize,
		MaxAge:     config.MaxAge,
		MaxBackups: config.MaxBackups,
		LocalTime:  true,
		Compress:   config.Compress,
	}

	var writeSyncer zapcore.WriteSyncer
	switch config.Output {
	case OutputFile:
		writeSyncer = zapcore.NewMultiWriteSyncer(zapcore.AddSync(lumberJackLogger))
	case OutputMmap:
		writeSyncer = zapcore.NewMultiWriteSyncer(zapcore.AddSync(mmapLogger))
	default:
		writeSyncer = zapcore.NewMultiWriteSyncer(zapcore.AddSync(os.Stdout))
	}

	level := zap.NewAtomicLevelAt(config.Level.ZapLevel())
	core := zapcore.NewCore(encoder, writeSyncer, level)

	options := []zap.Option{zap.AddCaller(), zap.AddCallerSkip(2)}
	if config.DisableStacktrace {
		options = append(options, zap.AddStacktrace(zap.FatalLevel))
	} else {
		options = append(options, zap.AddStacktrace(zap.ErrorLevel))
	}
	logger := zap.New(core, options...).Sugar()

	return &zapLogger{config: config, logger: logger, level: level}
}

// ZapLevel return a zap level.
func (lvl Level) ZapLevel() zapcore.Level {
	switch lvl {
	case LevelDebug:
		return zapcore.DebugLevel
	case LevelInfo:
		return zapcore.InfoLevel
	case LevelWarn:
		return zapcore.WarnLevel
	case LevelError:
		return zapcore.ErrorLevel
	case LevelPanic:
		return zapcore.PanicLevel
	case LevelFatal:
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

type zapLogger struct {
	config *Config
	logger *zap.SugaredLogger
	level  zap.AtomicLevel
}

func (l *zapLogger) With(args ...interface{}) Logger {
	l.logger = l.logger.With(args...)
	return l
}

func (l *zapLogger) SetLevel(lvl Level) {
	l.level.SetLevel(lvl.ZapLevel())
	l.config.Level = lvl
}

func (l *zapLogger) checkLevel() {
	if l.config.Level.ZapLevel() != l.level.Level() {
		l.SetLevel(l.config.Level)
	}
}

func (l *zapLogger) Debug(msg string, keyvals ...interface{}) {
	l.checkLevel()
	l.logger.Debugw(msg, keyvals...)
}

func (l *zapLogger) Info(msg string, keyvals ...interface{}) {
	l.checkLevel()
	l.logger.Infow(msg, keyvals...)
}

func (l *zapLogger) Warn(msg string, keyvals ...interface{}) {
	l.checkLevel()
	l.logger.Warnw(msg, keyvals...)
}

func (l *zapLogger) Error(msg string, keyvals ...interface{}) {
	l.checkLevel()
	l.logger.Errorw(msg, keyvals...)
}

func (l *zapLogger) Panic(msg string, keyvals ...interface{}) {
	l.checkLevel()
	l.logger.Panicw(msg, keyvals...)
}

func (l *zapLogger) Fatal(msg string, keyvals ...interface{}) {
	l.checkLevel()
	l.logger.Fatalw(msg, keyvals...)
}

func (l *zapLogger) Debugf(template string, args ...interface{}) {
	l.checkLevel()
	l.logger.Debugf(template, args...)
}

func (l *zapLogger) Infof(template string, args ...interface{}) {
	l.checkLevel()
	l.logger.Infof(template, args...)
}

func (l *zapLogger) Warnf(template string, args ...interface{}) {
	l.checkLevel()
	l.logger.Warnf(template, args...)
}

func (l *zapLogger) Errorf(template string, args ...interface{}) {
	l.checkLevel()
	l.logger.Errorf(template, args...)
}

func (l *zapLogger) Panicf(template string, args ...interface{}) {
	l.checkLevel()
	l.logger.Panicf(template, args...)
}

func (l *zapLogger) Fatalf(template string, args ...interface{}) {
	l.checkLevel()
	l.logger.Fatalf(template, args...)
}

func (l *zapLogger) Close() {
	_ = l.logger.Sync()
}
