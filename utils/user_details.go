package utils

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type UserConfig struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	Organization string `json:"organization"`
}

func getConfigFilePath() string {
	filePath := "/etc/blxrep/config.yaml"

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		LogError("Config file not found at " + filePath)
		return filePath
	}
	return filePath
}

func loadUserConfig() (UserConfig, error) {
	configPath := getConfigFilePath()
	file, err := os.Open(configPath)
	if err != nil {
		return UserConfig{}, err
	}
	defer file.Close()

	var config UserConfig
	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&config)
	return config, err
}

func saveUserConfig(config UserConfig) error {
	configPath := getConfigFilePath()
	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	return encoder.Encode(config)
}

func isValidEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,4}$`)
	return emailRegex.MatchString(email)
}

func GetUserInfo() UserConfig {
	config, err := loadUserConfig()
	if err == nil {
		fmt.Println("Existing user configuration found.")
		return config
	}

	reader := bufio.NewReader(os.Stdin)

	for config.Name == "" {
		fmt.Print("Enter your name: ")
		config.Name, _ = reader.ReadString('\n')
		config.Name = strings.TrimSpace(config.Name)
		if config.Name == "" {
			fmt.Println("Name cannot be empty. Please try again.")
		}
	}

	for config.Email == "" || !isValidEmail(config.Email) {
		fmt.Print("Enter your email: ")
		config.Email, _ = reader.ReadString('\n')
		config.Email = strings.TrimSpace(config.Email)
		if !isValidEmail(config.Email) {
			fmt.Println("Invalid email format. Please try again.")
		}
	}

	for config.Organization == "" {
		fmt.Print("Enter your organization: ")
		config.Organization, _ = reader.ReadString('\n')
		config.Organization = strings.TrimSpace(config.Organization)
		if config.Organization == "" {
			fmt.Println("Organization cannot be empty. Please try again.")
		}
	}

	err = saveUserConfig(config)
	if err != nil {
		LogError("Error saving user config: " + err.Error())
	}

	return config
}
