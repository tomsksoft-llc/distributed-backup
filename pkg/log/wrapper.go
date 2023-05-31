package log

import (
	"os"

	"github.com/sirupsen/logrus"
)

func SetupLogger() {
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.TraceLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: "15:04:05.000000000",
		FullTimestamp:   true,
	})
}

func Info(args ...any) {
	logrus.Info(args...)
}

func Infof(format string, args ...any) {
	logrus.Infof(format, args...)
}

func Error(args ...any) {
	logrus.Error(args...)
}

func Errorf(format string, args ...any) {
	logrus.Errorf(format, args...)
}

func Fatal(args ...any) {
	logrus.Fatal(args...)
}

func Fatalf(format string, args ...any) {
	logrus.Fatalf(format, args...)
}
