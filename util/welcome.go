package util

import (
	"flag"
	"fmt"

	"github.com/fatih/color"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	fiveSpaces = "     "
)

func PrintBanner(noColor bool) {
	color.NoColor = noColor
	c1, c2, c3, c4 := color.FgBlue, color.FgWhite, color.FgCyan, color.FgHiYellow
	pad := fiveSpaces + fiveSpaces
	lines := []string{
		pad + color.New(c1).Sprintf("  ___ ") + color.New(c4).Sprintf("_____") + color.New(c1).Sprintf(" _____ _      ") + color.New(c3).Sprintf("            _     _"),
		pad + color.New(c1).Sprintf(" / _ \\") + color.New(c4).Sprintf("_   _|") + color.New(c1).Sprintf(" ____| |     ") + color.New(c3).Sprintf("   __ _  __| | __| |     ___  _ __"),
		pad + color.New(c1).Sprintf("| | | |") + color.New(c4).Sprintf("| |") + color.New(c1).Sprintf(" |  _| | |     ") + color.New(c3).Sprintf("  / _` |/ _` |/ _` |") + color.New(c2).Sprintf("___") + color.New(c3).Sprintf(" / _ \\| '_ \\"),
		pad + color.New(c1).Sprintf("| |_| |") + color.New(c4).Sprintf("| |") + color.New(c1).Sprintf(" | |___| |___  ") + color.New(c3).Sprintf(" | (_| | (_| | (_| ") + color.New(c2).Sprintf("|___|") + color.New(c3).Sprintf(" (_) | | | |"),
		pad + color.New(c1).Sprintf(" \\___/ ") + color.New(c4).Sprintf("|_|") + color.New(c1).Sprintf(" |_____|_____\\ ") + color.New(c3).Sprintf("  \\__,_|\\__,_|\\__,_|") + color.New(c3).Sprintf("    \\___/|_| |_|\n"),
	}
	for _, line := range lines {
		line := line
		fmt.Println(line)
	}
}

// SetupLog tweak the default log to use custom time format and use colors if supported
func SetupLog(noColor bool) zapcore.LevelEnabler {
	var opts zap.Options
	zap.UseDevMode(true)(&opts)
	zap.ConsoleEncoder(func(c *zapcore.EncoderConfig) {
		c.EncodeCaller = func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(caller.TrimmedPath())
		}
		c.EncodeTime = zapcore.TimeEncoderOfLayout("01-02 15:04:05")
		if !noColor {
			c.EncodeLevel = zapcore.CapitalColorLevelEncoder
		}
	})(&opts)

	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	return opts.Level
}
