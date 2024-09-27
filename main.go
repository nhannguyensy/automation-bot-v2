package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/slack-go/slack"
)

// Task structure to handle static API tasks
type Task struct {
	Command string `json:"command"`
	URL     string `json:"url"`
	Method  string `json:"method"`
	User    string `json:"user,omitempty"`  // Optional for authentication
	Token   string `json:"token,omitempty"` // Optional for authentication
}

// JenkinsConfig structure for dynamic Jenkins deployments
type JenkinsConfig struct {
	User      string `json:"user,omitempty"`
	Token     string `json:"token,omitempty"`
	URLFormat string `json:"url_format"` // URL format with placeholders
}

// Config structure to hold Slack token, tasks, and Jenkins details
type Config struct {
	SlackToken string          `json:"slack_token"`
	Tasks      map[string]Task `json:"tasks"`   // Static API tasks
	Jenkins    JenkinsConfig   `json:"jenkins"` // Jenkins configuration for dynamic deployments
}

// Structure for parsing Slack's URL verification event
type ChallengeResponse struct {
	Challenge string `json:"challenge"`
}

// Load configuration from config.json
func loadConfig(filePath string) (*Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	// delay statement file.Close() until the function finish
	defer file.Close()

	byteValue, _ := ioutil.ReadAll(file)
	var config Config
	json.Unmarshal(byteValue, &config)

	return &config, nil
}

func main() {
	// Load configuration from config.json
	config, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	// Initialize Slack API with bot token from config
	api := slack.New(config.SlackToken)

	// HTTP handler for Slack events
	http.HandleFunc("/slack/events", func(w http.ResponseWriter, r *http.Request) {
		// Read the request body
		var body []byte
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			http.Error(w, "Can't read body", http.StatusBadRequest)
			return
		}

		// Parse the request body into a map to detect URL verification requests
		var parsedBody map[string]interface{}
		err = json.Unmarshal(body, &parsedBody)
		if err != nil {
			log.Printf("Error parsing JSON: %v", err)
			http.Error(w, "Can't parse JSON", http.StatusBadRequest)
			return
		}

		// Handle Slack URL verification challenge
		if parsedBody["type"] == "url_verification" {
			var challengeResp ChallengeResponse
			err = json.Unmarshal(body, &challengeResp)
			if err != nil {
				log.Printf("Error parsing challenge response: %v", err)
				http.Error(w, "Error parsing challenge", http.StatusBadRequest)
				return
			}
			// Respond with the challenge
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"challenge": challengeResp.Challenge,
			})
			return
		}

		// Log the entire incoming event for debugging
		log.Printf("Event received: %v", parsedBody)

		// Handle regular messages
		handleMessageEvent(api, parsedBody, config)
	})

	log.Println("Bot is running on port 8081...")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

// Handle incoming messages and trigger tasks
func handleMessageEvent(api *slack.Client, event map[string]interface{}, config *Config) {
	if event["event"] != nil {
		evt := event["event"].(map[string]interface{})

		// Ignore bot messages (the bot_id field is present if the message is from a bot)
		if evt["bot_id"] != nil {
			log.Println("Ignoring message from bot.")
			return
		}

		// Log the full event for debugging
		log.Printf("Full event received: %v", evt)

		if evt["type"] == "message" && evt["subtype"] == nil {
			log.Printf("Message received: %s", evt["text"])

			messageText := evt["text"].(string)
			channelID := evt["channel"].(string)

			// Log the channel ID and message
			log.Printf("Message received in channel: %s, message: %s", channelID, messageText)

			// Handle the "list" or "list command" request
			if strings.ToLower(messageText) == "list command" || strings.ToLower(messageText) == "list" {
				// Generate the list of available commands from the config file
				var commandsList string
				for cmd := range config.Tasks {
					commandsList += fmt.Sprintf("- %s\n", cmd)
				}

				// Send the list of commands back to the user
				response := fmt.Sprintf("Here are the available commands:\n%s", commandsList)
				_, _, err := api.PostMessage(channelID, slack.MsgOptionText(response, false))
				if err != nil {
					log.Printf("Error sending message to Slack: %v", err)
				}
				return
			}

			// Parse dynamic command like "deploy <service-name> <env>"
			if strings.HasPrefix(strings.ToLower(messageText), "deploy ") {
				args := strings.Split(messageText, " ")
				if len(args) == 3 {
					serviceName := args[1]
					env := args[2]
					// Add this log to check if the URL format is correctly loaded
					log.Printf("Jenkins URL format from config: %s", config.Jenkins.URLFormat)
					// Construct the dynamic Jenkins URL using the format from the config
					jenkinsURL := strings.Replace(config.Jenkins.URLFormat, "{service-name}", serviceName, 1)
					jenkinsURL = strings.Replace(jenkinsURL, "{env}", env, 1)

					log.Printf("Constructed Jenkins URL: %s", jenkinsURL) // Add this log for debugging

					// Execute the Jenkins job with Basic Authentication
					success := executeJenkinsJob(jenkinsURL, config.Jenkins.User, config.Jenkins.Token)

					// Send the execution result back to the channel
					var response string
					if success {
						response = fmt.Sprintf("Jenkins job for service '%s' in environment '%s' executed successfully.", serviceName, env)
					} else {
						response = fmt.Sprintf("Failed to execute Jenkins job for service '%s' in environment '%s'.", serviceName, env)
					}
					_, _, err := api.PostMessage(channelID, slack.MsgOptionText(response, false))
					if err != nil {
						log.Printf("Error sending message to Slack: %v", err)
					}
				} else {
					// Invalid deploy command format
					_, _, err := api.PostMessage(channelID, slack.MsgOptionText("Invalid deploy command format. Use: deploy <service-name> <env>", false))
					if err != nil {
						log.Printf("Error sending message to Slack: %v", err)
					}
				}
				return
			}

			// Handle static API tasks defined in the config.json
			userCommand := strings.ToLower(messageText)
			task, exists := config.Tasks[userCommand]

			if exists {
				log.Printf("Executing task for command: %s", userCommand)

				// Execute the task (send HTTP request to the task URL)
				success := executeTask(task)

				// Send the execution result back to the channel
				var response string
				if success {
					response = fmt.Sprintf("Task '%s' executed successfully.", task.Command)
				} else {
					response = fmt.Sprintf("Task '%s' failed to execute.", task.Command)
				}
				_, _, err := api.PostMessage(channelID, slack.MsgOptionText(response, false))
				if err != nil {
					log.Printf("Error sending message to Slack: %v", err)
				}

			} else {
				// Log if the command was not recognized and respond with a helpful message
				log.Printf("Unknown command: %s", userCommand)

				_, _, err := api.PostMessage(channelID, slack.MsgOptionText("I don't know your message. Please try again.", false))
				if err != nil {
					log.Printf("Error sending unrecognized message response: %v", err)
				}
			}
		}
	}
}

// Execute the Jenkins job using Basic Authentication for dynamic deploy
func executeJenkinsJob(url, user, token string) bool {
	// Prepare the POST request with Basic Authentication
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return false
	}

	// Add Basic Authentication header
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + token))
	req.Header.Add("Authorization", "Basic "+auth)

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error executing Jenkins job at %s: %v", url, err)
		return false
	}
	defer resp.Body.Close()

	// Check if the job executed successfully
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("Jenkins job executed successfully at %s, response status: %s", url, resp.Status)
		return true
	} else {
		log.Printf("Failed to execute Jenkins job at %s, response status: %s", url, resp.Status)
		return false
	}
}

// Execute the static API task
func executeTask(task Task) bool {
	var req *http.Request
	var err error

	if task.Method == "POST" {
		// Prepare the request for POST method
		req, err = http.NewRequest("POST", task.URL, nil)
		if task.User != "" && task.Token != "" {
			// Create the Basic Authentication header
			auth := base64.StdEncoding.EncodeToString([]byte(task.User + ":" + task.Token))
			req.Header.Add("Authorization", "Basic "+auth)
		}
	} else {
		// For GET, simply create the request
		req, err = http.NewRequest("GET", task.URL, nil)
	}

	if err != nil {
		log.Printf("Error creating request for task '%s': %v", task.Command, err)
		return false
	}

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error executing task '%s' at %s: %v", task.Command, task.URL, err)
		return false
	}
	defer resp.Body.Close()

	// Check if the task executed successfully based on the response status code
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("Task '%s' executed successfully at %s, response status: %s", task.Command, task.URL, resp.Status)
		return true
	} else {
		log.Printf("Task '%s' failed at %s, response status: %s", task.Command, task.URL, resp.Status)
		return false
	}
}
