**This is not an officially supported Google product. This is community supported
on a best-effort basis. Use at your own risk. This project is not eligible for the
[Google Open Source Software Vulnerability Rewards
Program](https://bughunters.google.com/open-source-security).**

---

# Looker CLI (looker-cli)

Looker CLI (`looker-cli`) is a robust, fast command-line interface (CLI) tool designed to navigate, manage, and automate Looker resources (Folders/Spaces, Looks, Dashboards, Users, and more) via the Looker API 4.0.

This is a Go-based reimplementation of the original Ruby `gzr` tool, utilizing the official Looker Go SDK for high performance and type safety.

---

## Table of Contents
1. [Installation](#installation)
2. [Authentication Guide](#authentication-guide)
3. [Profile Management](#profile-management)
4. [Global Flags](#global-flags)
5. [Complete Command Reference](#complete-command-reference)
   * [alert](#alert)
   * [attribute](#attribute)
   * [connection](#connection)
   * [dashboard](#dashboard)
   * [group](#group)
   * [look](#look)
   * [model](#model)
   * [permission](#permission)
   * [plan](#plan)
   * [project](#project)
   * [query](#query)
   * [role](#role)
   * [session](#session)
   * [space / folder](#space--folder)
   * [user](#user)

---

## Installation

To compile `looker-cli` from source:

```bash
# Build the binary in the current workspace
go build -o looker-cli ./cmd/looker-cli

# Verify the installation
./looker-cli version
```

> [!NOTE]
> If you want to uninstall the legacy `gzr` tool, use the command:
> ```bash
> gem uninstall gazer
> ```

---

## Authentication Guide

`looker-cli` supports several secure and flexible ways to connect to your Looker instance.

### 1. Interactive OAuth PKCE (Recommended for Users)
Log in interactively using a browser-based OAuth PKCE flow. You do not need to supply or save your API keys.

```bash
# Start the interactive OAuth login flow
./looker-cli session login --oauth --host your-looker-domain.com --port 443
```
This will:
1. Generate a secure PKCE verifier and challenge.
2. Spin up a temporary local callback server on port `7777` (listening strictly on local interface `127.0.0.1`).
3. Open your default web browser to authenticate with Looker.
4. Exchange the authentication code for an access token and store it in `~/.looker_auth`.

#### Setting up the OAuth Client Application in Looker
In version 26.10 and later, the **OAuth Client Application** `com.looker.cli` is
already registered. You may need to enable it under Admin-\>Platform-\>BI
Connectors. Under **Developer Tools** slide the toggle next to `Looker CLI`.

In version 26.8 and earlier, before you can use the `--oauth` login, your Looker Administrator must register `looker-cli` as an **OAuth Client Application** in your Looker instance:

1. Navigate to the **API Explorer** in Looker.
2. Click **Auth** then **Register OAuth App**.
3. Choose **Runit**.
4. Configure the following settings:
    * **client_guid**: `looker-cli` (This must match `looker-cli`'s default Client ID. If you register a different custom Client ID, you must pass it via the `--client-id` flag when logging in).
    * **Display Name**: `Looker CLI`
    * **Description**: `Looker CLI`
    * **Redirect URI**: Add `http://127.0.0.1:7777` (This is the temporary port spawned locally by `looker-cli` to retrieve the authorization code).
    * **Enabled**: Must be set to true.
5. Check the box "I understand that this API endpoint will change data."
6. Click **Run**.

Once registered, the `looker-cli session login --oauth` browser login will work seamlessly.

Alternately you can run the following if you have a `.netrc` with your
credentials already, or you can run this with one of the authentication methods
below:
```bash
echo << 'EOF' | ./looker-cli api auth register_oauth_client_app looker-cli - --host HOST [--port 443]
{
  "description": "Looker CLI",
  "display_name": "Looker CLI",
  "enabled": true,
  "redirect_uri": "http://127.0.0.1:7777"
}
EOF
```

### 2. Headless Token Authentication (Recommended for Scripts)
Once logged in, you can run automated scripts headlessly by referencing the stored token or passing one directly:

```bash
# Option A: Use the stored token file (~/.looker_auth)
./looker-cli user ls --token-file --host your-looker-domain.com --port 443

# Option B: Pass a static token directly via flag
./looker-cli user ls --token "YOUR_ACCESS_TOKEN" --host your-looker-domain.com
```

### 3. Netrc File (`.netrc`) (Recommended for API Keys)
Store your API Client ID and Client Secret securely in `~/.netrc` to keep them out of your shell history:

```text
machine your-looker-domain.com
login YOUR_API_CLIENT_ID
password YOUR_API_CLIENT_SECRET
```
`looker-cli` will automatically retrieve these credentials when connecting to `your-looker-domain.com`.

### 4. Environment Variables
```bash
export LOOKERSDK_CLIENT_ID="your_client_id"
export LOOKERSDK_CLIENT_SECRET="your_client_secret"
```

### 5. Direct Flags
```bash
./looker-cli user ls --client-id "ID" --client-secret "SECRET" --host your-looker-domain.com
```

---

## Profile Management

`looker-cli` supports configuration profiles to easily switch between different Looker instances or environments (e.g., dev, staging, production). Profiles are stored in `$HOME/.config/looker-cli/config.yaml`.

A profile stores the host, port, and optionally client credentials (`client_id`, `client_secret`), `verify_ssl` setting, and/or OAuth tokens (`access_token`, `refresh_token`, `expiration`).

### Managing Profiles

*   **Add a new profile**:
    ```bash
    ./looker-cli profile add my-dev --host dev.looker.com --port 19999 --client-id "ID" --client-secret "SECRET" --verify-ssl=false
    ```
    *Note: Only `--host` is required. If client credentials are not provided, they can be retrieved from `.netrc` or environment variables when the profile is used.*

*   **List all profiles**:
    ```bash
    ./looker-cli profile ls
    ```
    The active (default) profile is marked with an asterisk (`*`).

*   **Set a default profile**:
    ```bash
    ./looker-cli profile use my-dev
    ```

*   **Delete a profile**:
    ```bash
    ./looker-cli profile rm my-dev
    ```

### Using Profiles

Once you have profiles configured:

1.  **Default Profile**: If you don't specify a profile or connection flags, `looker-cli` will automatically use the default profile marked in `config.yaml`.
    ```bash
    # Uses default profile
    ./looker-cli user me
    ```

2.  **Explicit Profile**: Use the `--profile` global flag to use a specific profile.
    ```bash
    # Uses 'my-prod' profile
    ./looker-cli user me --profile my-prod
    ```

3.  **OAuth with Profiles**: If you use OAuth login with an active profile, the tokens will be saved directly into that profile in `config.yaml`.
    ```bash
    ./looker-cli session login --oauth --profile my-dev
    ```
    Subsequent commands using that profile will automatically reuse and refresh these tokens.

---

## Global Flags

Every command accepts the following optional global parameters to customize connection and behavior:

| Flag | Description | Default |
| :--- | :--- | :--- |
| `--host` | Looker Hostname | `localhost` |
| `--port` | Looker API Port | `19999` |
| `--profile` | Use a specific profile from `config.yaml` | `""` |
| `--ssl` | Use SSL/TLS for communication | `true` |
| `--verify-ssl` | Verify server SSL certificate | `true` |
| `--su` | Act as another user ID (Sudo) | `""` |
| `--timeout` | Seconds to wait for a response | `60` |
| `--token` | Access token to use for authentication | `""` |
| `--token-file` | Use access token stored in `~/.looker_auth` | `false` |
| `--client-id` | API Client ID | `""` |
| `--client-secret` | API Client Secret | `""` |
| `--header`, `-H` | Custom header to pass in the API call (can be specified multiple times) | `[]` |
| `--debug` | Enable verbose API logging | `false` |

---

## Complete Command Reference

### api
Make raw Looker API calls based on the Looker API 4.0 Swagger specification. 

This command dynamically exposes all endpoints of the Looker API, organized into subcommands by Tag, and operation IDs as sub-subcommands.

*   **List all Categories (Tags)**:
    ```bash
    ./looker-cli api help
    ```
*   **List operations under a Category**:
    ```bash
    ./looker-cli api query help
    ```
*   **Get detailed help for an operation** (including parameters and types):
    ```bash
    ./looker-cli api query create_query_task --help
    ```
*   **Make an API Call with Query Flags**:
    Required path parameters are passed as positional arguments. Optional parameters are passed as flags:
    ```bash
    ./looker-cli api user user 2456 --fields "id,email,first_name,last_name" --token-file --host your-domain.com
    ```
*   **Make an API Call with JSON Body**:
    If the command takes a JSON request body, it must be passed as a file path or `-` for standard input (stdin):
    ```bash
    ./looker-cli api query run_inline_query json my_query.json --token-file --host your-domain.com
    ```
*   **Describe JSON Request Body Schema**:
    If a command accepts a JSON request body, you can pass the `--describe-body` flag to output the full JSON schema of the expected body payload (bypassing other positional argument checks) and exit instantly:
    ```bash
    ./looker-cli api query create_query_task --describe-body
    ```

---

### alert
Commands pertaining to alerts.

*   **`ls`**: List alerts on the server.
    ```bash
    ./looker-cli alert ls --token-file --host your-domain.com
    ```
*   **`cat <alert_id>`**: Output JSON details about an alert.
    ```bash
    ./looker-cli alert cat 12 --token-file > alert.json
    ```
*   **`chown <alert_id> <user_id>`**: Change the owner of an alert.
    ```bash
    ./looker-cli alert chown 12 45 --token-file
    ```
*   **`disable <alert_id>`**: Disable an alert.
    ```bash
    ./looker-cli alert disable 12 --token-file
    ```
*   **`enable <alert_id>`**: Enable an alert.
    ```bash
    ./looker-cli alert enable 12 --token-file
    ```
*   **`follow <alert_id>`**: Start following an alert.
    ```bash
    ./looker-cli alert follow 12 --token-file
    ```
*   **`unfollow <alert_id>`**: Stop following an alert.
    ```bash
    ./looker-cli alert unfollow 12 --token-file
    ```
*   **`import <file>`**: Import an alert from a JSON file.
    ```bash
    ./looker-cli alert import alert.json --token-file
    ```
*   **`notifications`**: Retrieve alert notifications.
    ```bash
    ./looker-cli alert notifications --token-file
    ```
*   **`read <notification_id>`**: Mark a notification as read.
    ```bash
    ./looker-cli alert read 78 --token-file
    ```
*   **`threshold <alert_id> <value>`**: Change the threshold of an alert.
    ```bash
    ./looker-cli alert threshold 12 "100.5" --token-file
    ```
*   **`randomize`**: Randomize scheduled alert times on the server (useful for dev environments).
    ```bash
    ./looker-cli alert randomize --token-file
    ```
*   **`rm <alert_id>`**: Delete an alert.
    ```bash
    ./looker-cli alert rm 12 --token-file
    ```

---

### attribute
Commands pertaining to User Attributes.

*   **`ls`**: List all defined user attributes.
    ```bash
    ./looker-cli attribute ls --token-file
    ```
*   **`cat <attribute_id>`**: Output JSON information about an attribute.
    ```bash
    ./looker-cli attribute cat 5 --token-file
    ```
*   **`create <name> <type>`**: Create or modify a user attribute.
    ```bash
    ./looker-cli attribute create "my_attribute" "string" --token-file
    ```
*   **`import <file>`**: Import a user attribute from a file.
    ```bash
    ./looker-cli attribute import attribute.json --token-file
    ```
*   **`get_group_value <attribute_id> <group_id>`**: Retrieve a user attribute value for a specific group.
    ```bash
    ./looker-cli attribute get_group_value 5 10 --token-file
    ```
*   **`set_group_value <attribute_id> <group_id> <value>`**: Set a user attribute value for a specific group.
    ```bash
    ./looker-cli attribute set_group_value 5 10 "US-EAST" --token-file
    ```
*   **`rm <attribute_id>`**: Delete a user attribute.
    ```bash
    ./looker-cli attribute rm 5 --token-file
    ```

---

### connection
Commands pertaining to database connections and dialects.

*   **`ls`**: List all database connections.
    ```bash
    ./looker-cli connection ls --token-file
    ```
*   **`cat <connection_name>`**: Output the JSON representation of a database connection.
    ```bash
    ./looker-cli connection cat "my_db" --token-file
    ```
*   **`import <file>`**: Import a database connection from a JSON file.
    ```bash
    ./looker-cli connection import conn.json --token-file
    ```
*   **`dialects`**: List all supported SQL dialects.
    ```bash
    ./looker-cli connection dialects --token-file
    ```
*   **`test <connection_name>`**: Test a database connection.
    ```bash
    ./looker-cli connection test "my_db" --token-file
    ```
*   **`rm <connection_name>`**: Delete a database connection.
    ```bash
    ./looker-cli connection rm "my_db" --token-file
    ```

---

### dashboard
Commands pertaining to dashboards.

*   **`cat <dashboard_id>`**: Output JSON describing a dashboard.
    ```bash
    ./looker-cli dashboard cat 2 --token-file > dash.json
    ```
*   **`import <file> <folder_id>`**: Import a dashboard from a JSON file into a folder.
    ```bash
    ./looker-cli dashboard import dash.json 5 --token-file
    ```
*   **`import_lookml <lookml_dashboard_id> <folder_id>`**: Create a User Defined Dashboard (UDD) from a LookML dashboard.
    ```bash
    ./looker-cli dashboard import_lookml "model::my_dash" 5 --token-file
    ```
*   **`sync_lookml <lookml_dashboard_id> <folder_id>`**: Sync a UDD from its parent LookML dashboard.
    ```bash
    ./looker-cli dashboard sync_lookml "model::my_dash" 5 --token-file
    ```
*   **`mv <dashboard_id> <folder_id>`**: Move a dashboard to a different folder.
    ```bash
    ./looker-cli dashboard mv 2 10 --token-file
    ```
*   **`rm <dashboard_id>`**: Delete a dashboard.
    ```bash
    ./looker-cli dashboard rm 2 --token-file
    ```

---

### group
Commands pertaining to groups.

*   **`ls`**: List all groups.
    ```bash
    ./looker-cli group ls --token-file
    ```
*   **`member_groups <group_id>`**: List groups that are members of the given group.
    ```bash
    ./looker-cli group member_groups 3 --token-file
    ```
*   **`member_users <group_id>`**: List users that are members of the given group.
    ```bash
    ./looker-cli group member_users 3 --token-file
    ```

---

### look
Commands pertaining to Looks.

*   **`cat <look_id>`**: Output JSON describing a Look.
    ```bash
    ./looker-cli look cat 14 --token-file > look.json
    ```
*   **`import <file> <folder_id>`**: Import a Look from a JSON file into a folder.
    ```bash
    ./looker-cli look import look.json 5 --token-file
    ```
*   **`mv <look_id> <folder_id>`**: Move a Look to a different folder.
    ```bash
    ./looker-cli look mv 14 10 --token-file
    ```
*   **`rm <look_id>`**: Delete a Look.
    ```bash
    ./looker-cli look rm 14 --token-file
    ```

---

### model
Commands pertaining to LookML Models.

*   **`ls`**: List all models.
    ```bash
    ./looker-cli model ls --token-file
    ```
*   **`cat <model_name>`**: Output JSON representation of a model.
    ```bash
    ./looker-cli model cat "my_model" --token-file
    ```
*   **`import <file>`**: Import a LookML model from a JSON file.
    ```bash
    ./looker-cli model import model.json --token-file
    ```

#### model set
Commands for managing Model Sets (groups of models for user roles).
*   **`ls`**: List all model sets.
    ```bash
    ./looker-cli model set ls --token-file
    ```
*   **`cat <set_id>`**: Output JSON representation of a model set.
    ```bash
    ./looker-cli model set cat 2 --token-file
    ```
*   **`import <file>`**: Import a model set from a JSON file.
    ```bash
    ./looker-cli model set import set.json --token-file
    ```
*   **`rm <set_id>`**: Delete a model set.
    ```bash
    ./looker-cli model set rm 2 --token-file
    ```

---

### permission
Commands to retrieve permissions and manage Permission Sets.

*   **`ls`**: List all raw available system permissions.
    ```bash
    ./looker-cli permission ls --token-file
    ```
*   **`tree`**: Display system permissions in a hierarchical tree view.
    ```bash
    ./looker-cli permission tree --token-file
    ```

#### permission set
Commands for managing Permission Sets (permissions assigned to roles).
*   **`ls`**: List all permission sets.
    ```bash
    ./looker-cli permission set ls --token-file
    ```
*   **`cat <set_id>`**: Output JSON representation of a permission set.
    ```bash
    ./looker-cli permission set cat 4 --token-file
    ```
*   **`import <file>`**: Import a permission set from a JSON file.
    ```bash
    ./looker-cli permission set import perm_set.json --token-file
    ```
*   **`rm <set_id>`**: Delete a permission set.
    ```bash
    ./looker-cli permission set rm 4 --token-file
    ```

---

### plan
Commands pertaining to Scheduled Plans.

*   **`ls`**: List scheduled plans.
    ```bash
    ./looker-cli plan ls --token-file
    ```
*   **`cat <plan_id>`**: Output JSON representation of a scheduled plan.
    ```bash
    ./looker-cli plan cat 8 --token-file > plan.json
    ```
*   **`import <file>`**: Import a scheduled plan from a JSON file.
    ```bash
    ./looker-cli plan import plan.json --token-file
    ```
*   **`runit <plan_id>`**: Execute a saved scheduled plan immediately.
    ```bash
    ./looker-cli plan runit 8 --token-file
    ```
*   **`enable <plan_id>`**: Enable a scheduled plan.
    ```bash
    ./looker-cli plan enable 8 --token-file
    ```
*   **`disable <plan_id>`**: Disable a scheduled plan.
    ```bash
    ./looker-cli plan disable 8 --token-file
    ```
*   **`failures`**: Report all scheduled plans that failed in their most recent execution attempt.
    ```bash
    ./looker-cli plan failures --token-file
    ```
*   **`randomize`**: Randomize scheduled plan times (useful for dev instance testing).
    ```bash
    ./looker-cli plan randomize --token-file
    ```
*   **`rm <plan_id>`**: Delete a scheduled plan.
    ```bash
    ./looker-cli plan rm 8 --token-file
    ```

---

### project
Commands pertaining to LookML Projects.

*   **`ls`**: List all projects.
    ```bash
    ./looker-cli project ls --token-file
    ```
*   **`cat <project_id>`**: Output JSON information about a project.
    ```bash
    ./looker-cli project cat "my_project" --token-file
    ```
*   **`import <file>`**: Import a project from a file.
    ```bash
    ./looker-cli project import project.json --token-file
    ```
*   **`update <project_id> <file>`**: Update a project from a file definition.
    ```bash
    ./looker-cli project update "my_project" project_update.json --token-file
    ```
*   **`branch <project_id>`**: List the active Git branch or all branches of a project.
    ```bash
    ./looker-cli project branch "my_project" --token-file
    ```
*   **`checkout <project_id> <branch_name>`**: Checkout a specific Git branch for a project.
    ```bash
    ./looker-cli project checkout "my_project" "dev-feature" --token-file
    ```
*   **`deploy <project_id>`**: Deploy the active branch of a project to production.
    ```bash
    ./looker-cli project deploy "my_project" --token-file
    ```
*   **`deploy_key <project_id>`**: Generate/retrieve the Git deploy public key for the project.
    ```bash
    ./looker-cli project deploy_key "my_project" --token-file
    ```

#### Managing Project Files (`looker-cli project file`)
Manage the files inside your Looker project.

*   **`ls <project_id>`**: List all files in a project:
    ```bash
    ./looker-cli project file ls "my_project" --token-file
    ```
*   **`cat <project_id> <file_path>`**: Retrieve and print the raw text content of a LookML project file:
    ```bash
    ./looker-cli project file cat "my_project" "my_model.model.lkml" --token-file
    ```
*   **`create <project_id> <file_path> <content_file_or_->`**: Create a new LookML file inside a project (accepts payload from a local file or `-` for stdin):
    ```bash
    ./looker-cli project file create "my_project" "views/new_view.view.lkml" new_view.lkml --token-file
    ```
*   **`update <project_id> <file_path> <content_file_or_->`**: Overwrite/update an existing LookML file inside a project:
    ```bash
    ./looker-cli project file update "my_project" "views/new_view.view.lkml" updated_view.lkml --token-file
    ```
*   **`rm <project_id> <file_path>`**: Delete a file from a project:
    ```bash
    ./looker-cli project file rm "my_project" "views/new_view.view.lkml" --token-file
    ```

#### Managing Project Directories (`looker-cli project directory`)
Manage physical directories within your Looker LookML projects.

*   **`ls <project_id>`**: List all subdirectories inside a project:
    ```bash
    ./looker-cli project directory ls "my_project" --token-file
    ```
*   **`create <project_id> <dir_path>`**: Create a new subdirectory inside a project:
    ```bash
    ./looker-cli project directory create "my_project" "views/marketing" --token-file
    ```
*   **`rm <project_id> <dir_path>`**: Delete a directory from a project:
    ```bash
    ./looker-cli project directory rm "my_project" "views/marketing" --token-file
    ```

---

### query
Commands to retrieve and run queries.

*   **`runquery [QUERY_DEF]`**: Execute a query and return the results.
    ```bash
    # Run by ID
    ./looker-cli query runquery 123 --token-file

    # Run by slug
    ./looker-cli query runquery "abc123xyz" --token-file

    # Run using raw JSON query definition
    ./looker-cli query runquery '{"model":"dev","view":"users","fields":["id"]}' --token-file

    # Run using a local JSON query definition file
    ./looker-cli query runquery --file query_definition.json --token-file
    ```

---

### role
Commands pertaining to Roles.

*   **`ls`**: Display all roles.
    ```bash
    ./looker-cli role ls --token-file
    ```
*   **`cat <role_id>`**: Output the JSON representation of a role.
    ```bash
    ./looker-cli role cat 3 --token-file
    ```
*   **`create <name> <permission_set_id> <model_set_id>`**: Create a new role.
    ```bash
    ./looker-cli role create "Analyst" 4 2 --token-file
    ```
*   **`rm <role_id>`**: Delete a role.
    ```bash
    ./looker-cli role rm 3 --token-file
    ```

#### Managing Role Groups
*   **`group_ls <role_id>`**: List the groups assigned to a role.
    ```bash
    ./looker-cli role group_ls 3 --token-file
    ```
*   **`group_add <role_id> <group_id1,group_id2...>`**: Add groups to a role.
    ```bash
    ./looker-cli role group_add 3 10,11 --token-file
    ```
*   **`group_rm <role_id> <group_id1,group_id2...>`**: Remove groups from a role.
    ```bash
    ./looker-cli role group_rm 3 10 --token-file
    ```

#### Managing Role Users
*   **`user_ls <role_id>`**: List the users assigned to a role.
    ```bash
    ./looker-cli role user_ls 3 --token-file
    ```
*   **`user_add <role_id> <user_id1,user_id2...>`**: Add users to a role.
    ```bash
    ./looker-cli role user_add 3 45,46 --token-file
    ```
*   **`user_rm <role_id> <user_id1,user_id2...>`**: Remove users from a role.
    ```bash
    ./looker-cli role user_rm 3 45 --token-file
    ```

---

### session
Commands pertaining to API connections and workspace sessions.

*   **`login`**: Create a persistent session.
    ```bash
    # Standard login using API keys (prompts/uses config)
    ./looker-cli session login --host your-domain.com

    # OAuth PKCE login
    ./looker-cli session login --oauth --host your-domain.com --port 443
    ```
*   **`logout`**: End a persistent session and clear the local cached token.
    ```bash
    ./looker-cli session logout --host your-domain.com
    ```
*   **`get`**: Get data about your current Looker session.
    ```bash
    ./looker-cli session get --token-file --host your-domain.com
    ```
*   **`update <dev | production>`**: Change the workspace ID of your current session.
    ```bash
    ./looker-cli session update dev --token-file --host your-domain.com
    ```

---

### space / folder
Commands pertaining to spaces (now known as Folders in Looker). *Supports alias: `folder`.*

*   **`top`**: Retrieve the top-level (root) folders.
    ```bash
    ./looker-cli space top --token-file
    ```
*   **`ls [space_id]`**: List the looks, dashboards, and folders in the given space (defaults to root/top spaces if omitted).
    ```bash
    ./looker-cli space ls 5 --token-file
    ```
*   **`cat <space_id>`**: Output JSON describing a folder.
    ```bash
    ./looker-cli space cat 5 --token-file
    ```
*   **`create <name> [parent_id]`**: Create a new subspace/folder under the specified parent.
    ```bash
    ./looker-cli space create "Marketing Folders" 1 --token-file
    ```
*   **`tree [space_id]`**: Display child spaces, dashboards, and looks in a tree format.
    ```bash
    ./looker-cli space tree 1 --token-file
    ```
*   **`export <space_id>`**: Recursively download all looks, dashboards, and folders inside a space to your local filesystem.
    ```bash
    ./looker-cli space export 5 --token-file
    ```
*   **`rm <space_id>`**: Delete a folder.
    ```bash
    ./looker-cli space rm 5 --token-file
    ```

---

### user
Commands pertaining to Users.

*   **`me`**: Show information for your currently authenticated user.
    ```bash
    ./looker-cli user me --token-file --host your-domain.com
    ```
*   **`ls`**: List all users on the system.
    ```bash
    ./looker-cli user ls --token-file --host your-domain.com
    ```
*   **`ls --last-login`**: List users including their true most recent login timestamp, dynamically resolved across all auth types (SAML, LDAP, Google OAuth, OIDC, Email, etc.):
    ```bash
    ./looker-cli user ls --last-login --token-file --host your-domain.com
    ```
*   **`cat <user_id>`**: Output JSON details about a user.
    ```bash
    ./looker-cli user cat 45 --token-file > user_45.json
    ```
*   **`enable <user_id>`**: Enable a user.
    ```bash
    ./looker-cli user enable 45 --token-file
    ```
*   **`disable <user_id>`**: Disable a user.
    ```bash
    ./looker-cli user disable 45 --token-file
    ```
*   **`delete <user_id>`**: Delete a user.
    ```bash
    ./looker-cli user delete 45 --token-file
    ```
