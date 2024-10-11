package build

import (
	"fmt"
	"runtime"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"

	"github.com/kedify/otel-add-on/util"
)

var (
	version   = "main"
	gitCommit string
)

// Version returns the current git SHA of commit the binary was built from
func Version() string {
	return version
}

// GitCommit stores the current commit hash
func GitCommit() string {
	return gitCommit
}

func PrintComponentInfo(logger logr.Logger, lvl zapcore.LevelEnabler, component string) {
	logger.Info(fmt.Sprintf("%s Version: %s", component, Version()))
	logger.Info(fmt.Sprintf("%s Commit: %s", component, GitCommit()))
	logger.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	logger.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	if lvl != nil {
		logger.Info(fmt.Sprintf("Logger: %+v", lvl))
		logger.Info(fmt.Sprintf("Debug enabled: %+v", lvl.Enabled(util.DebugLvl)))
	}
	fmt.Println()
}
