package log

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hkjojo/go-toolkits/log/encoder"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Config ...
type Config struct {
	Path   string
	Level  string
	Fields map[string]string

	MaxSize       int
	MaxBackups    int
	MaxAge        int
	DisableStdout bool
	Compress      bool
	Format        string // json/console/text
	ForbitTime    bool
	ForbitLevel   bool
	Caller        bool
	Prefix        string
	Kafka         *KafkaConfig
	WebHook       []*WebHookConfig
	RotateDay     int
}

// SugaredLogger ..
type SugaredLogger struct {
	*zap.SugaredLogger
}

// Logger ..
type Logger struct {
	*zap.Logger
	config *Config
}

var (
	// std is the name of the standard logger in stdlib `log`
	logger = &Logger{}
	sugger = &SugaredLogger{}
)

// CoreType ..
type CoreType int

// CoreDefine
const (
	CoreUndefine CoreType = iota
	CoreTelegram
	CoreDingDing
	CoreKafKa
)

func init() {
	l, _ := zap.NewDevelopment()
	logger = &Logger{l, &Config{}}
	sugger = logger.Sugar()
}

// AddFields ..
func (c *Config) AddFields(fs map[string]string) {
	if c.Fields == nil {
		c.Fields = make(map[string]string)
	}
	for k, v := range fs {
		c.Fields[k] = v
	}
}

// Sugar copy zaplog
func (log *Logger) Sugar() *SugaredLogger {
	return &SugaredLogger{log.Logger.Sugar()}
}

// Fields ...
type Fields map[string]interface{}

// New ..
func New(config *Config) (*Logger, error) {
	var (
		lvl        zapcore.Level
		err        error
		hooks      []zapcore.WriteSyncer
		rotatehook *rotatelogs.RotateLogs
		ecoder     zapcore.Encoder
		timeKey    = "time"
		levelKey   = "level"
		msgKey     = "msg"
	)
	if config.Level != "" {
		lvl = ParseLevel(config.Level)
		if err != nil {
			return nil, err
		}
	}

	if config.Path != "" {
		dir := getDir(config.Path)
		if isPathNotExist(dir) {
			if err = os.MkdirAll(dir, os.ModePerm); err != nil {
				return nil, err
			}
		}

		if config.MaxSize != 0 {
			hook := lumberjack.Logger{
				Filename:   config.Path,       // log path
				MaxSize:    config.MaxSize,    // file max size：M
				MaxBackups: config.MaxBackups, // max backup file num
				MaxAge:     config.MaxAge,     // file age
				Compress:   config.Compress,   // compress gz
			}
			hooks = append(hooks, zapcore.AddSync(&hook))
		}

		if config.RotateDay != 0 {
			var fn = config.Path
			if !filepath.IsAbs(fn) {
				v, err := filepath.Abs(fn)
				if err != nil {
					return nil, err
				}
				fn = v
			}

			rotatehook, err = rotatelogs.New(
				fn+".%Y%m%d",
				rotatelogs.WithLinkName(fn),
				rotatelogs.WithMaxAge(time.Hour*24*time.Duration(config.MaxAge)),
				rotatelogs.WithRotationTime(time.Hour*24*time.Duration(config.RotateDay)),
			)
			hooks = append(hooks, zapcore.AddSync(rotatehook))
		}
	}

	if config.DisableStdout == false {
		hooks = append(hooks, os.Stdout)
	}

	if config.ForbitTime {
		timeKey = ""
	}

	if config.ForbitLevel {
		levelKey = ""
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:       timeKey,
		LevelKey:      levelKey,
		NameKey:       "logger",
		CallerKey:     "caller",
		MessageKey:    msgKey,
		StacktraceKey: "stacktrace",
		LineEnding:    zapcore.DefaultLineEnding,
		EncodeLevel:   zapcore.CapitalLevelEncoder,
		EncodeTime: func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.Format("2006-01-02 15:04:05.999999999 -07"))
		},
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	switch strings.ToLower(config.Format) {
	case "json":
		ecoder = zapcore.NewJSONEncoder(encoderConfig)
	case "fix":
		ecoder = encoder.NewFixEncoder(encoderConfig)
	default:
		ecoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	var cores []zapcore.Core
	cores = append(cores, zapcore.NewCore(
		ecoder,
		zapcore.NewMultiWriteSyncer(hooks...),
		lvl,
	))

	for _, cfg := range config.WebHook {
		cores = append(cores, NewWebHookCore(cfg, encoderConfig))
	}

	if config.Kafka != nil {
		core, err := NewKafkaCore(config, encoderConfig)
		if err != nil {
			return nil, err
		}
		cores = append(cores, core)
	}

	core := zapcore.NewTee(cores...)
	var l *zap.Logger
	l = zap.New(core)
	if config.Caller {
		l = l.WithOptions(zap.AddCaller())
	}

	return &Logger{l, config}, nil
}

// Init ...
func Init(config *Config) error {
	var err error
	logger, err = New(config)
	if err != nil {
		return err
	}
	sugger = logger.Sugar()
	return nil
}

func isPathNotExist(path string) bool {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return true
		}
	}
	return false
}

func getDir(path string) string {
	paths := strings.Split(path, "/")
	return strings.Join(
		paths[:len(paths)-1],
		"/",
	)
}

// CombineFields ..
func CombineFields(src, src2 map[string]string) (dst map[string]string) {
	dst = make(map[string]string)
	for k, v := range src {
		dst[k] = v
	}
	for k, v := range src2 {
		dst[k] = v
	}
	return
}

// ParseLevel .. parse level
func ParseLevel(loglevel string) zapcore.Level {
	var lv zapcore.Level
	lv.UnmarshalText([]byte(loglevel))
	return lv
}
