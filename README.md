# Jira to Things3 Sync Tool

This tool synchronizes Jira issues assigned to you with a Things3 project. It provides two-way sync:

- Jira issues assigned to you are created as todos in Things3
- When a todo is marked as complete in Things3, you are unassigned from the corresponding Jira issue

## Prerequisites

- Go 1.16 or later
- Jira account with API token
- Things3 installed on your Mac
- The project in Things3 where you want to sync the issues

## Setup

1. Clone this repository
2. Set the following environment variables:
   - `JIRA_URL`: Your Jira instance URL
   - `JIRA_USERNAME`: Your Jira email
   - `JIRA_TOKEN`: Your Jira API token (create one at <https://id.atlassian.com/manage/api-tokens>)
   - `THINGS_PROJECT`: The name of your Things3 project where issues should be synced

4. Install dependencies:

   ```bash
   go mod tidy
   ```

## Usage

Run the tool:

```bash
go run main.go
```

The tool will:

1. Fetch all Jira issues assigned to you
2. Create corresponding todos in Things3
3. Monitor Things3 for completed todos and unassign you from the corresponding Jira issues

## Notes

- The tool uses Things3 URL scheme to create todos
- Make sure Things3 is installed and running when using this tool
- The sync is one-way for now (Jira → Things3)
