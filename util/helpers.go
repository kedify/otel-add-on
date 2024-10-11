package util

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/kedify/otel-add-on/types"
)

const DebugLvl = -1

func Map[I, R any](input []I, f func(I) R) []R {
	result := make([]R, len(input))
	for i := range input {
		result[i] = f(input[i])
	}
	return result
}

func FlatMap[I, R any](input []I, f func(I) []R) []R {
	var result []R
	for _, v := range input {
		result = append(result, f(v)...)
	}
	return result
}

func Filter[I any](input []I, f func(I) bool) []I {
	var result []I
	for _, v := range input {
		if f(v) {
			result = append(result, v)
		}
	}
	return result
}

func Filter2[I any](input []I, f func(I) bool) []I {
	return FlatMap(input, func(v I) []I {
		if f(v) {
			return []I{v}
		} else {
			return []I{}
		}
	})
}

func ResolveOsEnvBool(envName string, defaultValue bool) (bool, error) {
	valueStr, found := os.LookupEnv(envName)

	if found && valueStr != "" {
		return strconv.ParseBool(valueStr)
	}

	return defaultValue, nil
}

func ResolveOsEnvInt(envName string, defaultValue int) (int, error) {
	valueStr, found := os.LookupEnv(envName)

	if found && valueStr != "" {
		return strconv.Atoi(valueStr)
	}

	return defaultValue, nil
}

func ResolveOsEnvDuration(envName string) (*time.Duration, error) {
	valueStr, found := os.LookupEnv(envName)

	if found && valueStr != "" {
		value, err := time.ParseDuration(valueStr)
		return &value, err
	}

	return nil, nil
}

func CheckTimeOp(op types.OperationOverTime) error {
	switch op {
	case types.OpLastOne, types.OpRate, types.OpCount, types.OpAvg, types.OpMin, types.OpMax:
		return nil
	default:
		return fmt.Errorf("unknown OperationOverTime:%s", op)
	}
}

func IsDebug(lvl zapcore.LevelEnabler) bool {
	return lvl != nil && lvl.Enabled(DebugLvl)
}

// SplitAfter works as the same way as strings.SplitAfter() but the separator is regexp
func SplitAfter(s string, re *regexp.Regexp) (r []string) {
	if re == nil {
		return
	}
	re.ReplaceAllStringFunc(s, func(x string) string {
		s = strings.ReplaceAll(s, x, "::"+x)
		return s
	})
	for _, x := range strings.Split(s, "::") {
		if x != "" {
			r = append(r, x)
		}
	}
	return
}
