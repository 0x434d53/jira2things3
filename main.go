package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/andygrunwald/go-jira"
)

type Config struct {
	JiraURL       string
	JiraUsername  string
	JiraToken     string
	ThingsProject string
}

func loadConfig() (*Config, error) {
	config := &Config{
		JiraURL:       os.Getenv("JIRA_URL"),
		JiraUsername:  os.Getenv("JIRA_USERNAME"),
		JiraToken:     os.Getenv("JIRA_TOKEN"),
		ThingsProject: os.Getenv("THINGS_PROJECT"),
	}

	if config.JiraURL == "" || config.JiraUsername == "" || config.JiraToken == "" || config.ThingsProject == "" {
		return nil, fmt.Errorf("missing required environment variables")
	}

	return config, nil
}

func createThingsTodo(title, notes, jiraKey string) error {
	// First check if todo already exists
	script := fmt.Sprintf(`
		tell application "Things3"
			repeat with t in to dos of project "%s"
				if ((notes of t as string) contains "JIRA-ID: %s") then
					return "true"
			end if
		end repeat
			return "false"
		end tell
	`, os.Getenv("THINGS_PROJECT"), jiraKey)

	log.Printf("Executing AppleScript: %s", script)
	checkCmd := exec.Command("osascript", "-e", script)

	output, err := checkCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			log.Printf("AppleScript stderr: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("failed to check for existing todo: %v", err)
	}

	log.Printf("AppleScript output: %s", string(output))

	// If todo exists (output contains "true"), skip creation
	if strings.TrimSpace(string(output)) == "true" {
		log.Printf("Todo for issue %s already exists, skipping", jiraKey)
		return nil
	}

	// Add unique identifier to notes
	notes = fmt.Sprintf("%s\n\nJIRA-ID: %s", notes, jiraKey)

	// URL encode all parameters
	params := url.Values{}
	params.Add("title", title)
	params.Add("notes", notes)
	params.Add("list", os.Getenv("THINGS_PROJECT"))

	// Things3 URL scheme for creating a todo
	scheme := "things:///add?" + strings.ReplaceAll(params.Encode(), "+", "%20")

	// Use the 'open' command to trigger Things3 URL scheme
	cmd := exec.Command("open", scheme)
	return cmd.Run()
}

func markThingsTodoCompleted(jiraKey string) error {
	script := fmt.Sprintf(`
		tell application "Things3"
			repeat with t in to dos of project "%s"
				if ((notes of t as string) contains "JIRA-ID: %s") then
					set status of t to completed
					return "true"
				end if
			end repeat
			return "false"
		end tell
	`, os.Getenv("THINGS_PROJECT"), jiraKey)

	output, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return fmt.Errorf("failed to mark todo as completed: %v", err)
	}

	if strings.TrimSpace(string(output)) == "true" {
		log.Printf("Marked todo for issue %s as completed", jiraKey)
	}
	return nil
}

func syncJiraToThings(client *jira.Client, config *Config) error {
	// Get all todos with JIRA IDs from Things3
	getAllTodosScript := fmt.Sprintf(`
		tell application "Things3"
			set todoList to {}
			repeat with t in to dos of project "%s"
				if status of t is not completed then
					set noteText to (notes of t as string)
					if noteText contains "JIRA-ID: " then
						set jiraKey to text ((offset of "JIRA-ID: " in noteText) + 9) thru -1 of noteText
						copy jiraKey to end of todoList
					end if
				end if
			end repeat
			return todoList
		end tell
	`, os.Getenv("THINGS_PROJECT"))

	output, err := exec.Command("osascript", "-e", getAllTodosScript).Output()
	if err != nil {
		return fmt.Errorf("failed to get todos from Things3: %v", err)
	}

	// Parse the comma-separated list of Jira keys
	existingJiraKeys := make(map[string]bool)
	if len(output) > 0 {
		keys := strings.Split(strings.TrimSpace(string(output)), ", ")
		for _, key := range keys {
			existingJiraKeys[key] = true
		}
	}

	// Get currently assigned issues
	jql := "assignee = currentUser() AND status != Done"
	issues, _, err := client.Issue.Search(jql, &jira.SearchOptions{})
	if err != nil {
		return fmt.Errorf("failed to search issues: %v", err)
	}

	// Track which issues are still assigned to us
	currentIssues := make(map[string]bool)
	for _, issue := range issues {
		currentIssues[issue.Key] = true

		if !existingJiraKeys[issue.Key] {
			// Create new todo for newly assigned issues
			title := fmt.Sprintf("[%s] %s", issue.Key, issue.Fields.Summary)
			notes := fmt.Sprintf("Jira Issue: %s/browse/%s\n\n%s",
				config.JiraURL, issue.Key, issue.Fields.Description)

			if err := createThingsTodo(title, notes, issue.Key); err != nil {
				log.Printf("Failed to create todo for issue %s: %v", issue.Key, err)
			}
		}
	}

	// Mark todos as completed for issues no longer assigned to us
	for jiraKey := range existingJiraKeys {
		if !currentIssues[jiraKey] {
			if err := markThingsTodoCompleted(jiraKey); err != nil {
				log.Printf("Failed to mark todo as completed for issue %s: %v", jiraKey, err)
			}
		}
	}

	return nil
}

func syncThingsToJira(client *jira.Client, config *Config) error {
	// Get completed todos with JIRA IDs from Things3
	getCompletedTodosScript := fmt.Sprintf(`
		tell application "Things3"
			set todoList to {}
			repeat with t in to dos of project "%s"
				if status of t is completed then
					set noteText to (notes of t as string)
					if noteText contains "JIRA-ID: " then
						set jiraKey to text ((offset of "JIRA-ID: " in noteText) + 9) thru -1 of noteText
						copy jiraKey to end of todoList
					end if
				end if
			end repeat
			return todoList
		end tell
	`, os.Getenv("THINGS_PROJECT"))

	output, err := exec.Command("osascript", "-e", getCompletedTodosScript).Output()
	if err != nil {
		return fmt.Errorf("failed to get completed todos from Things3: %v", err)
	}

	// Process completed todos
	if len(output) > 0 {
		keys := strings.Split(strings.TrimSpace(string(output)), ", ")
		for _, key := range keys {
			// Skip empty keys
			if key == "" {
				continue
			}

			// Get the issue
			issue, _, err := client.Issue.Get(key, nil)
			if err != nil {
				log.Printf("Failed to get issue %s: %v", key, err)
				continue
			}

			// If the issue is assigned to us, unassign it
			if issue.Fields.Assignee != nil && issue.Fields.Assignee.EmailAddress == config.JiraUsername {
				update := &jira.Issue{
					Key: key,
					Fields: &jira.IssueFields{
						Assignee: &jira.User{Name: ""},
					},
				}

				_, _, err := client.Issue.Update(update)
				if err != nil {
					log.Printf("Failed to unassign issue %s: %v", key, err)
					continue
				}
				log.Printf("Unassigned issue %s", key)
			}
		}
	}

	return nil
}

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	tp := jira.BasicAuthTransport{
		Username: config.JiraUsername,
		Password: config.JiraToken,
	}

	client, err := jira.NewClient(tp.Client(), config.JiraURL)
	if err != nil {
		log.Fatalf("Failed to create Jira client: %v", err)
	}

	// Run both syncs
	if err := syncJiraToThings(client, config); err != nil {
		log.Fatalf("Failed to sync Jira to Things: %v", err)
	}

	if err := syncThingsToJira(client, config); err != nil {
		log.Fatalf("Failed to sync Things to Jira: %v", err)
	}

	log.Println("Sync completed successfully")
}
