package log

type Config struct {
	Level             Level  // Level is the minimum enabled logging level.
	Output            Output // Output determines where the log should be written to, value: "console" or "file"
	Filename          string // Filename is the file to write logs to.
	MaxSize           int    // MaxSize is the maximum size in megabytes of the log file before it gets rotated.
	MaxAge            int    // MaxAge is the maximum number of days to retain old log files based on the timestamp encoded in their filename.
	MaxBackups        int    // MaxBackups is the maximum number of old log files to retain.
	Compress          bool   // Compress determines if the rotated log files should be compressed using gzip.
	DevMode           bool   // DevMode if true -> print colourful log in console and files.
	DisableStacktrace bool
}

var (
	defaultMaxSize    = 100
	defaultMaxAge     = 30
	defaultMaxBackups = 10

	defaultFilename = "./log/main.log"
	defaultConfig   = &Config{Level: LevelInfo}

	ExtraFields   = []interface{}{}
	DefaultLogger = New(defaultConfig).With(ExtraFields...)
)
