package discovery

import (
	golog "log"
	"time"
)

var (
	blue   = string([]byte{27, 91, 57, 55, 59, 52, 52, 109}) // INF
	yellow = string([]byte{27, 91, 57, 55, 59, 52, 51, 109}) // WRN
	red    = string([]byte{27, 91, 57, 55, 59, 52, 49, 109}) // ERR
	cyan   = string([]byte{27, 91, 57, 55, 59, 52, 54, 109}) // ???
	reset  = string([]byte{27, 91, 48, 109})
)

type logger struct {
	// DisableColor specifies if log output in console should use colors
	DisableColor bool // = false
	// LogLevel specifies what logs should be output. Level 0 enables all output, level 1 disables VRB
	// output, level 2 disables VRN and INF, and so on... Level 4 will disable all known output types
	// (VRB, INF, WAR, ERR), and level 5 or higher will disable all output (including output with
	// custom log type).
	LogLevel int // = 0
}

func (l logger) logV(logMessage string) {
	if l.LogLevel <= 0 {
		l.log("VRB", logMessage)
	}
}

func (l logger) logI(logMessage string) {
	if l.LogLevel <= 1 {
		l.log("INF", logMessage)
	}
}

func (l logger) logW(logMessage string) {
	if l.LogLevel <= 2 {
		l.log("WRN", logMessage)
	}
}

func (l logger) logE(logMessage string) {
	if l.LogLevel <= 3 {
		l.log("ERR", logMessage)
	}
}

func (l logger) log(logType string, logMessage string) {

	var color string
	if !l.DisableColor {
		switch logType {
		case "VRB":
			color = reset
			break
		case "INF":
			color = blue
			break
		case "WRN":
			color = yellow
			break
		case "ERR":
			color = red
			break
		default:
			color = cyan
		}
	} else {
		color = reset
	}

	if l.LogLevel <= 4 {
		golog.Printf("[Kumuluz-discovery] %v |%s %1s %s| %s\n",
			time.Now().Format("2006/01/02 15:04:05"),
			color, logType, reset,
			logMessage,
		)
	}
}
