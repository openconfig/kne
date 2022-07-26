package cptx

import (
	scraplibase "github.com/scrapli/scrapligo/driver/base"
)

// https://github.com/openconfig/kne/issues/163
// Default privilege levels in scrapli do not recognize hostnames with '.' in them and would hang.
// Define custom privilege levels here to include such usecases.
// Remove after fast-forwarding scrapli version.
const (
	execPrivLevel = "exec"
	// privExecPrivLevel = "privilege_exec"
	configPrivLevel = "configuration"
)

var customPrivLevels = map[string]*scraplibase.PrivilegeLevel{
	"exec": {
		Pattern:        `(?im)^({\w+:\d}\n){0,1}[\w\-@()./:]{1,63}>\s?$`,
		Name:           execPrivLevel,
		PreviousPriv:   "",
		Deescalate:     "",
		Escalate:       "",
		EscalateAuth:   false,
		EscalatePrompt: "",
	},
	"configuration": {
		Pattern:        `(?im)^({\w+:\d}\[edit\]\n){0,1}[\w\-@()./:]{1,63}#\s?$`,
		Name:           configPrivLevel,
		PreviousPriv:   execPrivLevel,
		Deescalate:     "exit configuration-mode",
		Escalate:       "configure",
		EscalateAuth:   false,
		EscalatePrompt: "",
	},
	"configuration_exclusive": {
		Pattern:        `(?im)^({\w+:\d}\[edit\]\n){0,1}[\w\-@()./:]{1,63}#\s?$`,
		Name:           "configuration_exclusive",
		PreviousPriv:   execPrivLevel,
		Deescalate:     "exit configuration-mode",
		Escalate:       "configure exclusive",
		EscalateAuth:   false,
		EscalatePrompt: "",
	},
	"configuration_private": {
		Pattern:        `(?im)^({\w+:\d}\[edit\]\n){0,1}[\w\-@()./:]{1,63}#\s?$`,
		Name:           "configuration_private",
		PreviousPriv:   execPrivLevel,
		Deescalate:     "exit configuration-mode",
		Escalate:       "configure exclusive",
		EscalateAuth:   false,
		EscalatePrompt: "",
	},
	"shell": {
		Pattern:            `(?im)^.*[%$]\s?$`,
		PatternNotContains: []string{"root"},
		Name:               "shell",
		PreviousPriv:       execPrivLevel,
		Deescalate:         "exit",
		Escalate:           "start shell",
		EscalateAuth:       false,
		EscalatePrompt:     "",
	},
	"root_shell": {
		Pattern:        `(?im)^.*root@[[:ascii:]]*?:?[[:ascii:]]*?[%#]\s?$`,
		Name:           "root_shell",
		PreviousPriv:   execPrivLevel,
		Deescalate:     "exit",
		Escalate:       "start shell user root",
		EscalateAuth:   true,
		EscalatePrompt: `(?im)^[pP]assword:\s?$`,
	},
}
