package utils

import (
	"fmt"

	"github.com/fatih/color"
)

const logoTemplate = `                                              
 _      _                              
| |    | |                             
| |__  | |  _   _   ____  _____  ____  
|  _ \ | | ( \ / ) / ___)| ___ ||  _ \ 
| |_) )| |  ) X ( | |    | ____|| |_| |
|____/  \_)(_/ \_)|_|    |_____)|  __/ 
                                |_|  
   made with ♥ by team xmigrate                                                                                                                                                        
`

func PrintAnimatedLogo() {
	cyan := color.New(color.FgMagenta).SprintFunc()

	// Clear the console (this may not work on all systems)
	fmt.Print("\033[H\033[2J")

	logo := fmt.Sprint(logoTemplate)
	fmt.Println(cyan(logo))

}

func GetDiskBanner() string {
	return `  
 _      _                              
| |    | |                             
| |__  | |  _   _   ____  _____  ____  
|  _ \ | | ( \ / ) / ___)| ___ ||  _ \ 
| |_) )| |  ) X ( | |    | ____|| |_| |
|____/  \_)(_/ \_)|_|    |_____)|  __/ 
                                |_|         
`
}
