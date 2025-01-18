package utils

import (
	"fmt"

	"github.com/fatih/color"
)

const logoTemplate = `
██   ██ ███    ███ ██████  ███████ ██████  ██      ██  ██████  █████  ████████  ██████  ██████  
 ██ ██  ████  ████ ██   ██ ██      ██   ██ ██      ██ ██      ██   ██    ██    ██    ██ ██   ██ 
  ███   ██ ████ ██ ██████  █████   ██████  ██      ██ ██      ███████    ██    ██    ██ ██████  
 ██ ██  ██  ██  ██ ██   ██ ██      ██      ██      ██ ██      ██   ██    ██    ██    ██ ██   ██ 
██   ██ ██      ██ ██   ██ ███████ ██      ███████ ██  ██████ ██   ██    ██     ██████  ██   ██ 
                                                                                                                                                             
`

func PrintAnimatedLogo() {
	cyan := color.New(color.FgMagenta).SprintFunc()

	// Clear the console (this may not work on all systems)
	fmt.Print("\033[H\033[2J")

	logo := fmt.Sprint(logoTemplate)
	fmt.Println(cyan(logo))

}

func GetDiskBanner() string {
	return ` __  __ __  __  ___  ___  ___  _     ___   ___    _    _____   ___   ___ 
 \ \/ /|  \/  || _ \| __|| _ \| |   |_ _| / __|  /_\  |_   _| / _ \ | _ \
  >  < | |\/| ||   /| _| |  _/| |__  | | | (__  / _ \   | |  | (_) ||   /
 /_/\_\|_|  |_||_|_\|___||_|  |____||___| \___|/_/ \_\  |_|   \___/ |_|_\
`
}
