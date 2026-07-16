#!/usr/bin/env bash

#
# create_jira_tickets.sh — bulk-create Jira tickets from a CSV file.
#
# CSV format (first row is a header, ignored). Columns:
#     Title,Description,Assignee,Labels
#   - Title       : issue summary (required)
#   - Description : issue description (plain text)
#   - Assignee    : email address of the assignee (optional; blank = unassigned)
#                   The script resolves the email to a Jira accountId for you.
#   - Labels      : semicolon-separated labels, no spaces (optional)
#                   e.g. FinOps;cost-optimization   (spaces are auto-converted to '-')
#
# Usage:
#     ./create_jira_tickets.sh [path/to/tickets.csv]
#
# If no CSV path is given it defaults to ./jira_tickets.csv (next to this script).
#
# Requirements: bash, curl, jq, python3  (all standard on macOS + most Linux)
#
# Authentication:
#   On first run the script prompts for your Jira site URL, email, and an API
#   token, then offers to save them to ~/.jira_config (chmod 600) so future runs
#   authenticate automatically. Create a token at:
#     https://id.atlassian.com/manage-profile/security/api-tokens
# ---------------------------------------------------------------------------

set -euo pipefail

CONFIG_FILE="${HOME}/.jira_config"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CSV_FILE="${1:-${SCRIPT_DIR}/jira_tickets.csv}"
ISSUE_TYPE="${JIRA_ISSUE_TYPE:-Task}"   # override with: JIRA_ISSUE_TYPE=Story ./create_jira_tickets.sh

# ---------------------------------------------------------------------------
# 0. Dependency check
# ---------------------------------------------------------------------------

for dep in curl jq python3; do
 if ! command -v "$dep" >/dev/null 2>&1; then
   echo "ERROR: '$dep' is required but not installed." >&2
   exit 1
 fi
done

if [[ ! -f "$CSV_FILE" ]]; then
 echo "ERROR: CSV file not found: $CSV_FILE" >&2
 echo "Pass the path as the first argument, or place jira_tickets.csv next to this script." >&2
 exit 1
fi

# ---------------------------------------------------------------------------
# 1. Auto-authentication (load saved config, else prompt & offer to save)
# ---------------------------------------------------------------------------

if [[ -f "$CONFIG_FILE" ]]; then
 # shellcheck disable=SC1090
 source "$CONFIG_FILE"
fi

# Environment variables take precedence over the config file if set.
JIRA_BASE_URL="${JIRA_BASE_URL:-}"
JIRA_EMAIL="${JIRA_EMAIL:-}"
JIRA_API_TOKEN="${JIRA_API_TOKEN:-}"
JIRA_DEFAULT_BOARD_URL="${JIRA_DEFAULT_BOARD_URL:-}"
JIRA_ASSIGNEE_FALLBACK_MODE="${JIRA_ASSIGNEE_FALLBACK_MODE:-unassigned}"
JIRA_CREATE_CONFIRM_MODE="${JIRA_CREATE_CONFIRM_MODE:-auto}"

prompt_if_empty() {
 local var_name="$1" prompt="$2" secret="${3:-}" val=""
 if [[ -z "${!var_name}" ]]; then
   if [[ "$secret" == "secret" ]]; then
     read -r -s -p "$prompt" val; echo
   else
     read -r -p "$prompt" val
   fi
   printf -v "$var_name" '%s' "$val"
 fi
}

prompt_if_empty JIRA_BASE_URL  "Jira site URL (e.g. https://your-domain.atlassian.net): "
prompt_if_empty JIRA_EMAIL     "Jira account email: "
prompt_if_empty JIRA_API_TOKEN "Jira API token (input hidden): " secret

save_config_value() {
 local key="$1"
 local value="$2"
 local tmp

 umask 077
 tmp="$(mktemp)"

 if [[ -f "$CONFIG_FILE" ]]; then
   awk -v key="$key" -v value="$value" '
     BEGIN { updated = 0 }
     $0 ~ "^" key "=" {
       printf "%s=\"%s\"\n", key, value
       updated = 1
       next
     }
     { print }
     END {
       if (!updated) {
         printf "%s=\"%s\"\n", key, value
       }
     }
   ' "$CONFIG_FILE" > "$tmp"
 else
   printf '%s="%s"\n' "$key" "$value" > "$tmp"
 fi

 mv "$tmp" "$CONFIG_FILE"
 chmod 600 "$CONFIG_FILE"
}

# Strip any trailing slash from the base URL.

JIRA_BASE_URL="${JIRA_BASE_URL%/}"
if [[ -z "$JIRA_BASE_URL" || -z "$JIRA_EMAIL" || -z "$JIRA_API_TOKEN" ]]; then
 echo "ERROR: URL, email, and API token are all required." >&2
 exit 1
fi

# Offer to persist config if it wasn't already saved.

if [[ ! -f "$CONFIG_FILE" ]]; then
 read -r -p "Save these credentials to ${CONFIG_FILE} for next time? [y/N] " save
 if [[ "$save" =~ ^[Yy]$ ]]; then
   umask 077
   cat > "$CONFIG_FILE" <<EOF
JIRA_BASE_URL="$JIRA_BASE_URL"
JIRA_EMAIL="$JIRA_EMAIL"
JIRA_API_TOKEN="$JIRA_API_TOKEN"
EOF

   chmod 600 "$CONFIG_FILE"
   echo "Saved to $CONFIG_FILE (permissions 600)."
 fi
fi

AUTH="${JIRA_EMAIL}:${JIRA_API_TOKEN}"
# Small helper for authenticated GET requests.

api_get() {
 curl -sS -u "$AUTH" -H "Accept: application/json" "$@"
}

# ---------------------------------------------------------------------------
# 2. Verify credentials
# ---------------------------------------------------------------------------

echo "Verifying credentials against ${JIRA_BASE_URL} ..."
me="$(api_get "${JIRA_BASE_URL}/rest/api/2/myself" || true)"

if ! echo "$me" | jq -e '.accountId' >/dev/null 2>&1; then
 echo "ERROR: Authentication failed. Check your URL, email, and API token." >&2
 echo "Response: $me" >&2
 exit 1
fi

REQUESTER_DISPLAY_NAME="$(echo "$me" | jq -r '.displayName // empty')"
echo "Authenticated as ${REQUESTER_DISPLAY_NAME}."
REQUESTER_ACCOUNT_ID="$(echo "$me" | jq -r '.accountId // empty')"

if [[ "$JIRA_ASSIGNEE_FALLBACK_MODE" != "unassigned" && "$JIRA_ASSIGNEE_FALLBACK_MODE" != "requester" ]]; then
 echo "Warning: JIRA_ASSIGNEE_FALLBACK_MODE must be 'unassigned' or 'requester'. Using 'unassigned'." >&2
 JIRA_ASSIGNEE_FALLBACK_MODE="unassigned"
fi

if [[ "$JIRA_CREATE_CONFIRM_MODE" != "auto" && "$JIRA_CREATE_CONFIRM_MODE" != "ask-each-time" ]]; then
 echo "Warning: JIRA_CREATE_CONFIRM_MODE must be 'auto' or 'ask-each-time'. Using 'auto'." >&2
 JIRA_CREATE_CONFIRM_MODE="auto"
fi

echo
echo "Assignee fallback mode when CSV assignee is blank or unresolved:"
echo "  1) unassigned"
echo "  2) requester (${REQUESTER_DISPLAY_NAME})"
read -r -p "Choose [1/2] (Enter keeps '${JIRA_ASSIGNEE_FALLBACK_MODE}'): " assignee_mode_choice
case "${assignee_mode_choice}" in
  1) JIRA_ASSIGNEE_FALLBACK_MODE="unassigned" ;;
  2) JIRA_ASSIGNEE_FALLBACK_MODE="requester" ;;
  "") ;;
  *)
    echo "Invalid choice; keeping '${JIRA_ASSIGNEE_FALLBACK_MODE}'."
    ;;
esac

if [[ -f "$CONFIG_FILE" ]]; then
 read -r -p "Save assignee fallback mode (${JIRA_ASSIGNEE_FALLBACK_MODE}) to ${CONFIG_FILE}? [y/N] " save_assignee_mode
 if [[ "$save_assignee_mode" =~ ^[Yy]$ ]]; then
   save_config_value "JIRA_ASSIGNEE_FALLBACK_MODE" "$JIRA_ASSIGNEE_FALLBACK_MODE"
   echo "Saved assignee fallback mode to ${CONFIG_FILE}."
 fi
fi

echo
echo "Ticket creation mode:"
echo "  1) auto (create all tickets without per-ticket confirmation)"
echo "  2) ask-each-time (prompt before creating each ticket)"
read -r -p "Choose [1/2] (Enter keeps '${JIRA_CREATE_CONFIRM_MODE}'): " create_mode_choice
case "${create_mode_choice}" in
  1) JIRA_CREATE_CONFIRM_MODE="auto" ;;
  2) JIRA_CREATE_CONFIRM_MODE="ask-each-time" ;;
  "") ;;
  *)
    echo "Invalid choice; keeping '${JIRA_CREATE_CONFIRM_MODE}'."
    ;;
esac

if [[ -f "$CONFIG_FILE" ]]; then
 read -r -p "Save ticket creation mode (${JIRA_CREATE_CONFIRM_MODE}) to ${CONFIG_FILE}? [y/N] " save_create_mode
 if [[ "$save_create_mode" =~ ^[Yy]$ ]]; then
   save_config_value "JIRA_CREATE_CONFIRM_MODE" "$JIRA_CREATE_CONFIRM_MODE"
   echo "Saved ticket creation mode to ${CONFIG_FILE}."
 fi
fi

# ---------------------------------------------------------------------------
# 3. Choose a board -> resolve its project key
# ---------------------------------------------------------------------------

echo
echo "Fetching boards ..."
boards_json="$(api_get "${JIRA_BASE_URL}/rest/agile/1.0/board?maxResults=100")"
board_count="$(echo "$boards_json" | jq '.values | length')"
board_id=""
board_name=""

if [[ -n "${JIRA_DEFAULT_BOARD_URL:-}" ]]; then
 echo "Default board URL: ${JIRA_DEFAULT_BOARD_URL}"
fi

echo "Note: Jira board results are paginated; this list may not include all boards."
if [[ "$board_count" -eq 0 ]]; then
 echo "No boards found on this site (or your token lacks Agile access)." >&2
 read -r -p "Enter board URL, board ID, or project KEY (e.g. NHE): " board_input
else
 echo "Available boards:"
 echo "$boards_json" | jq -r '.values[] | "\(.id)\t\(.name)\t[\(.location.projectKey // "?")]"' | nl -w2 -s'. '
 echo
 read -r -p "Enter board NUMBER, board URL, board ID, or project KEY [default URL if blank]: " board_input
fi

if [[ -z "${board_input:-}" && -n "${JIRA_DEFAULT_BOARD_URL:-}" ]]; then
 board_input="$JIRA_DEFAULT_BOARD_URL"
 echo "Using saved default board URL."
fi

if [[ -n "${board_input:-}" ]]; then
 if [[ "${board_input}" =~ ^[0-9]+$ && "$board_count" -gt 0 ]]; then
   idx=$((board_input - 1))
   PROJECT_KEY="$(echo "$boards_json" | jq -r ".values[$idx].location.projectKey // empty")"
   board_name="$(echo "$boards_json" | jq -r ".values[$idx].name // empty")"
   board_id="$(echo "$boards_json" | jq -r ".values[$idx].id // empty")"
   if [[ -n "$PROJECT_KEY" ]]; then
     echo "Selected board: ${board_name}  ->  project ${PROJECT_KEY}"
   fi
 else
   candidate="$board_input"
   if [[ "$candidate" =~ /boards/([0-9]+) ]]; then
     board_id="${BASH_REMATCH[1]}"
   elif [[ "$candidate" =~ ^[0-9]+$ ]]; then
     board_id="$candidate"
   fi

   if [[ -n "$board_id" ]]; then
     board_resp="$(api_get "${JIRA_BASE_URL}/rest/agile/1.0/board/${board_id}" || true)"
     PROJECT_KEY="$(echo "$board_resp" | jq -r '.location.projectKey // empty' 2>/dev/null || true)"
     board_name="$(echo "$board_resp" | jq -r '.name // empty' 2>/dev/null || true)"
     if [[ -n "$PROJECT_KEY" ]]; then
       echo "Resolved board: ${board_name}  ->  project ${PROJECT_KEY}"
     fi
   fi

   if [[ -z "${PROJECT_KEY:-}" ]]; then
     if [[ "$candidate" =~ ^[A-Za-z][A-Za-z0-9_\-]*$ ]]; then
       PROJECT_KEY="$candidate"
       echo "Using project key: ${PROJECT_KEY}"
     fi
   fi
 fi
fi

if [[ -z "${PROJECT_KEY:-}" ]]; then
 echo "Could not resolve a project key from your selection." >&2
 read -r -p "Enter the project KEY manually (e.g. NHE): " PROJECT_KEY
fi

if [[ -z "${PROJECT_KEY:-}" ]]; then
 echo "ERROR: No project key selected." >&2
 exit 1
fi

if [[ -n "${board_id:-}" ]]; then
 default_board_url="${JIRA_BASE_URL}/jira/software/c/projects/${PROJECT_KEY}/boards/${board_id}"
 read -r -p "Save ${default_board_url} as your default board URL? [y/N] " save_board
 if [[ "$save_board" =~ ^[Yy]$ ]]; then
   save_config_value "JIRA_DEFAULT_BOARD_URL" "$default_board_url"
   echo "Saved default board URL to ${CONFIG_FILE}."
 fi
fi

# ---------------------------------------------------------------------------
# 3b. Assignee email -> accountId resolver (cached)
# ---------------------------------------------------------------------------

declare -A ACCOUNT_CACHE

resolve_account_id() {
 local email="$1"
 [[ -z "$email" ]] && { echo ""; return; }
 if [[ -n "${ACCOUNT_CACHE[$email]:-}" ]]; then
   echo "${ACCOUNT_CACHE[$email]}"; return
 fi

 local enc resp aid
 enc="$(python3 -c 'import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))' "$email")"
 resp="$(api_get "${JIRA_BASE_URL}/rest/api/3/user/search?query=${enc}" || true)"

 # Only accept exact matches; never fall back to the first search result.
 aid="$(echo "$resp" | jq -r --arg e "$email" \
   'map(select(.emailAddress==$e or .displayName==$e)) | .[0].accountId // empty' 2>/dev/null || true)"
 ACCOUNT_CACHE[$email]="$aid"
 echo "$aid"
}

# ---------------------------------------------------------------------------
# 4. Create one issue per CSV row
# ---------------------------------------------------------------------------

echo
echo "Creating tickets in project ${PROJECT_KEY} (issue type: ${ISSUE_TYPE}) ..."
echo "Assignee fallback mode: ${JIRA_ASSIGNEE_FALLBACK_MODE}"
echo "Ticket creation mode: ${JIRA_CREATE_CONFIRM_MODE}"
echo
created=0
failed=0
skipped=0

# python3 parses the CSV robustly (quoted commas, newlines) and emits one

# tab-separated title<TAB>description<TAB>assignee<TAB>labels per row.

while IFS=$'\t' read -r title description assignee labels; do
 [[ -z "$title" ]] && continue

 # Build the fields object incrementally.
 payload="$(jq -n \
   --arg pk "$PROJECT_KEY" \
   --arg summary "$title" \
   --arg desc "$description" \
   --arg itype "$ISSUE_TYPE" \
   '{fields: {project: {key: $pk}, summary: $summary, description: $desc, issuetype: {name: $itype}}}')"

 # Labels: split on ';', drop blanks, replace spaces with '-'.
 if [[ -n "$labels" ]]; then
   labels_json="$(echo "$labels" | jq -R 'split(";") | map(gsub("^\\s+|\\s+$";"") | gsub("\\s+";"-")) | map(select(length>0))')"
   payload="$(echo "$payload" | jq --argjson l "$labels_json" '.fields.labels = $l')"
 fi

 # Assignee: resolve explicit assignee to accountId; fallback is configurable.
 aid=""
 if [[ -n "$assignee" ]]; then
   aid="$(resolve_account_id "$assignee")"
   if [[ -z "$aid" ]]; then
     echo "  ! Could not resolve assignee '${assignee}'."
   fi
 fi

 if [[ -z "$aid" && "$JIRA_ASSIGNEE_FALLBACK_MODE" == "requester" ]]; then
   aid="$REQUESTER_ACCOUNT_ID"
 fi

 if [[ -n "$aid" ]]; then
   payload="$(echo "$payload" | jq --arg a "$aid" '.fields.assignee = {accountId: $a}')"
 elif [[ -z "$assignee" ]]; then
   echo "  - No assignee provided — creating unassigned."
 else
   echo "  - Creating unassigned."
 fi

 if [[ "$JIRA_CREATE_CONFIRM_MODE" == "ask-each-time" ]]; then
   read -r -p "Create ticket for '${title}'? [Y/n/q] " create_choice
   case "$create_choice" in
     [Qq])
       echo "Stopping at your request."
       break
       ;;
     [Nn])
       echo "  - Skipped ${title}"
       skipped=$((skipped + 1))
       continue
       ;;
     *)
       ;;
   esac
 fi

 resp="$(curl -sS -u "$AUTH" -X POST \
   -H "Content-Type: application/json" \
   --data "$payload" \
   "${JIRA_BASE_URL}/rest/api/2/issue" || true)"

 key="$(echo "$resp" | jq -r '.key // empty')"
 if [[ -n "$key" ]]; then
   echo "  ok ${key}  ${title}"
   echo "      ${JIRA_BASE_URL}/browse/${key}"
   created=$((created + 1))
 else
   echo "  x FAILED  ${title}"
   echo "      $(echo "$resp" | jq -c '.errors // .errorMessages // .')"
   failed=$((failed + 1))
 fi
done < <(python3 -c '

import csv, sys
with open(sys.argv[1], newline="") as f:
   reader = csv.reader(f)
   next(reader, None)  # skip header
   for row in reader:
       if not row:
           continue
       def col(i):
           return row[i].strip() if len(row) > i else ""
       vals = [col(0), col(1), col(2), col(3)]
       vals = [v.replace("\t", " ").replace("\n", " ") for v in vals]
       print("\t".join(vals))
' "$CSV_FILE")

echo
echo "Done. Created: ${created}, Failed: ${failed}, Skipped: ${skipped}."
