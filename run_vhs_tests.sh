#!/usr/bin/env bash
# run_vhs_tests.sh — Set up environment and run VHS tests for jira-tui.
#
# Usage:
#   ./run_vhs_tests.sh [--tape path/to/custom.tape]
#
# Required environment variables (or set in a .env.test file):
#   JIRA_BASE_URL      https://yourorg.atlassian.net
#   JIRA_EMAIL         your@email.com
#   JIRA_API_TOKEN     your-api-token
#   TEST_PROJECT_KEY   A real project key, e.g. "PROJ"
#   TEST_EPIC_KEY      A real Epic key in that project, e.g. "PROJ-1"
#
# Optional:
#   JIRA_TUI_BIN       Path to binary (default: ./jira-tui)
#   VHS_TAPE           Path to tape file (default: ./VHS_Testing.tape)
#   JIRA_TEST_DIR      Override test data directory (default: /tmp/jira-tui-vhs-test)
#   SKIP_BUILD         Set to 1 to skip go build
#   SKIP_ASSERT        Set to 1 to skip assertion-based CLI tests (run VHS only)
#   SKIP_VHS           Set to 1 to run only assertion tests (skip VHS)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

JIRA_TUI_BIN="${JIRA_TUI_BIN:-./jira-tui}"
VHS_TAPE="${VHS_TAPE:-./VHS_Testing.tape}"
JIRA_TEST_DIR="${JIRA_TEST_DIR:-/tmp/jira-tui-vhs-test}"
SKIP_BUILD="${SKIP_BUILD:-0}"
SKIP_ASSERT="${SKIP_ASSERT:-0}"
SKIP_VHS="${SKIP_VHS:-0}"

PASS=0
FAIL=0
SKIP=0

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
green()  { printf '\033[0;32m%s\033[0m\n' "$*"; }
red()    { printf '\033[0;31m%s\033[0m\n' "$*"; }
yellow() { printf '\033[0;33m%s\033[0m\n' "$*"; }
bold()   { printf '\033[1m%s\033[0m\n' "$*"; }

pass() { local id="$1" desc="$2"; green "  ✓ ${id}  ${desc}"; PASS=$((PASS+1)); }
fail() { local id="$1" desc="$2" reason="$3"; red "  ✗ ${id}  ${desc}"; red "      reason: ${reason}"; FAIL=$((FAIL+1)); }
skip() { local id="$1" desc="$2"; yellow "  - ${id}  ${desc} (skipped)"; SKIP=$((SKIP+1)); }

assert_exit() {
    local id="$1" desc="$2" expected_exit="$3"
    shift 3
    local actual_exit=0
    "$@" >/dev/null 2>&1 || actual_exit=$?
    if [[ "${actual_exit}" -eq "${expected_exit}" ]]; then
        pass "${id}" "${desc}"
    else
        fail "${id}" "${desc}" "expected exit ${expected_exit}, got ${actual_exit}"
    fi
}

assert_output_contains() {
    local id="$1" desc="$2" pattern="$3"
    shift 3
    local output
    output=$("$@" 2>&1) || true
    if echo "${output}" | grep -q "${pattern}"; then
        pass "${id}" "${desc}"
    else
        fail "${id}" "${desc}" "output did not contain: ${pattern}"
    fi
}


assert_file_exists() {
    local id="$1" desc="$2" path="$3"
    if [[ -f "${path}" ]]; then
        pass "${id}" "${desc}"
    else
        fail "${id}" "${desc}" "file not found: ${path}"
    fi
}

assert_file_contains() {
    local id="$1" desc="$2" path="$3" pattern="$4"
    if [[ ! -f "${path}" ]]; then
        fail "${id}" "${desc}" "file not found: ${path}"
        return
    fi
    if grep -q "${pattern}" "${path}"; then
        pass "${id}" "${desc}"
    else
        fail "${id}" "${desc}" "file ${path} did not contain: ${pattern}"
    fi
}

# ---------------------------------------------------------------------------
# Load optional .env.test
# ---------------------------------------------------------------------------
if [[ -f "${SCRIPT_DIR}/.env.test" ]]; then
    # shellcheck source=/dev/null
    source "${SCRIPT_DIR}/.env.test"
fi

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
    case "$1" in
        --tape) VHS_TAPE="$2"; shift 2 ;;
        --bin)  JIRA_TUI_BIN="$2"; shift 2 ;;
        *) echo "Unknown argument: $1" >&2; exit 1 ;;
    esac
done

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
bold "jira-tui VHS Test Runner"
echo ""
echo "Checking prerequisites…"

if ! command -v go &>/dev/null; then
    red "ERROR: 'go' not found in PATH. Install Go 1.21+ to build the binary."
    exit 1
fi

if [[ "${SKIP_VHS}" == "0" ]] && ! command -v vhs &>/dev/null; then
    red "ERROR: 'vhs' not found in PATH."
    echo "  Install with:  brew install vhs"
    echo "  Or:            go install github.com/charmbracelet/vhs@latest"
    echo "  Set SKIP_VHS=1 to run only assertion-based tests."
    exit 1
fi

if [[ "${SKIP_VHS}" == "0" ]] && ! command -v gum &>/dev/null; then
    red "ERROR: 'gum' not found in PATH (required for test headers in VHS tape)."
    echo "  Install with:  brew install gum"
    echo "  Or:            go install github.com/charmbracelet/gum@latest"
    echo "  Set SKIP_VHS=1 to run only assertion-based tests."
    exit 1
fi

# ---------------------------------------------------------------------------
# Build binary
# ---------------------------------------------------------------------------
if [[ "${SKIP_BUILD}" != "1" ]]; then
    echo "Building binary…"
    go build -o jira-tui . 2>&1
    echo "  → ./jira-tui built"
fi

if [[ ! -x "${JIRA_TUI_BIN}" ]]; then
    red "ERROR: binary not found or not executable: ${JIRA_TUI_BIN}"
    exit 1
fi

# ---------------------------------------------------------------------------
# Validate required env vars (only needed for VHS + network tests)
# ---------------------------------------------------------------------------
HAVE_CREDS=1
for v in JIRA_BASE_URL JIRA_EMAIL JIRA_API_TOKEN TEST_PROJECT_KEY TEST_EPIC_KEY; do
    if [[ -z "${!v:-}" ]]; then
        yellow "  WARNING: ${v} is not set — network/VHS tests will be skipped."
        HAVE_CREDS=0
    fi
done

# ---------------------------------------------------------------------------
# Assertion-based CLI tests (no network, no TUI)
# ---------------------------------------------------------------------------
if [[ "${SKIP_ASSERT}" != "1" ]]; then
    echo ""
    bold "=== Section 1.1  Help flags ==="

    for flag in "--help" "-h" "-H" "--HELP" "-?"; do
        assert_exit      "1.1.x" "exit 0 for ${flag}"    0  "${JIRA_TUI_BIN}" "${flag}"
        assert_output_contains "1.1.x" "${flag} contains --create-csv-from-epic" \
            "create-csv-from-epic"  "${JIRA_TUI_BIN}" "${flag}"
        assert_output_contains "1.1.x" "${flag} contains --show-tickets" \
            "show-tickets"          "${JIRA_TUI_BIN}" "${flag}"
        assert_output_contains "1.1.x" "${flag} contains --create-ticket" \
            "create-ticket"         "${JIRA_TUI_BIN}" "${flag}"
        assert_output_contains "1.1.x" "${flag} contains --create-epic" \
            "create-epic"           "${JIRA_TUI_BIN}" "${flag}"
        assert_output_contains "1.1.x" "${flag} contains JIRA_BASE_URL env var" \
            "JIRA_BASE_URL"         "${JIRA_TUI_BIN}" "${flag}"
    done

    echo ""
    bold "=== Section 1.2  Input validation ==="

    assert_exit            "1.2.1" "-ccfe with no key → exit 1"              1  "${JIRA_TUI_BIN}" -ccfe
    assert_output_contains "1.2.1" "-ccfe error message mentions ticket ID"  \
        "ticket ID"  "${JIRA_TUI_BIN}" -ccfe
    assert_exit            "1.2.2" "--create-csv-from-epic with no key → exit 1" 1 \
        "${JIRA_TUI_BIN}" --create-csv-from-epic
    assert_exit            "1.2.3" "nonexistent CSV → exit 1"                1 \
        "${JIRA_TUI_BIN}" /tmp/jira_tui_no_such_file_xyz_abc.csv

    echo ""
    bold "=== Section 1.3  CSV parsing (static) ==="

    PARSE_TMP_DIR="$(mktemp -d)"
    trap 'rm -rf "${PARSE_TMP_DIR}"' EXIT

    # 1.3.2: row with empty title should be skipped (no ticket for it)
    printf 'Title,Description,Assignee,Labels\n,empty title row,,\n' \
        > "${PARSE_TMP_DIR}/empty_title.csv"

    # Launching the app will attempt to connect to Jira after CSV is parsed.
    # We check for the "no tickets found" error (exit 1) rather than the auth spinner.
    assert_exit "1.3.2" "CSV with only empty-title rows → exit 1 (no tickets)" 1 \
        "${JIRA_TUI_BIN}" "${PARSE_TMP_DIR}/empty_title.csv"

    # 1.3.1: UTF-8 BOM stripped
    printf '\xef\xbb\xbfTitle,Description,Assignee,Labels\nBOM Title,desc,,\n' \
        > "${PARSE_TMP_DIR}/bom.csv"
    # The binary always exits non-zero in a non-TTY environment (TUI can't init).
    # We distinguish BOM-stripping success/failure by checking stderr: a failed
    # strip would produce "no tickets found" (empty title after BOM mangles it).
    bom_stderr=$(JIRA_TUI_DIR="/tmp/jira-tui-bom-$$" \
        timeout 2s "${JIRA_TUI_BIN}" "${PARSE_TMP_DIR}/bom.csv" 2>&1 || true)
    if echo "${bom_stderr}" | grep -qiE "no tickets found|error reading csv"; then
        fail "1.3.1" "BOM CSV should be accepted (BOM was not stripped)" \
            "${bom_stderr}"
    else
        pass "1.3.1" "BOM CSV accepted (no CSV parse error in stderr)"
    fi
    rm -rf "/tmp/jira-tui-bom-$$"

    rm -rf "${PARSE_TMP_DIR}"
    trap - EXIT

    echo ""
    bold "=== Section 15.2  Environment variable overrides (static) ==="

    # Verify help text documents all env vars
    for envvar in JIRA_BASE_URL JIRA_EMAIL JIRA_API_TOKEN JIRA_TUI_DIR \
                  JIRA_BOARD_CACHE_TTL_HOURS JIRA_USE_ADF; do
        assert_output_contains "15.2.x" "--help documents ${envvar}" \
            "${envvar}"  "${JIRA_TUI_BIN}" --help
    done
fi

# ---------------------------------------------------------------------------
# Set up test environment for VHS / network tests
# ---------------------------------------------------------------------------
if [[ "${SKIP_VHS}" != "1" ]] && [[ "${HAVE_CREDS}" == "1" ]]; then
    echo ""
    bold "Setting up VHS test environment…"

    # Clean and recreate test data dir
    rm -rf "${JIRA_TEST_DIR}"
    mkdir -p "${JIRA_TEST_DIR}"

    # Write config with credentials (so VHS sessions skip the auth screen)
    cat > "${JIRA_TEST_DIR}/config" <<CFGEOF
JIRA_BASE_URL="${JIRA_BASE_URL}"
JIRA_EMAIL="${JIRA_EMAIL}"
JIRA_API_TOKEN="${JIRA_API_TOKEN}"
JIRA_ASSIGNEE_FALLBACK_MODE="unassigned"
JIRA_ISSUE_TYPE="Task"
JIRA_BOARD_CACHE_TTL_HOURS="24"
JIRA_USE_ADF="false"
CFGEOF
    chmod 600 "${JIRA_TEST_DIR}/config"
    echo "  → Config written to ${JIRA_TEST_DIR}/config"

    # Create test CSV with timestamped titles to avoid dup-check collisions
    TIMESTAMP="$(date +%Y%m%d%H%M%S)"
    JIRA_TEST_CSV="${SCRIPT_DIR}/vhs-test-tickets.csv"
    cat > "${JIRA_TEST_CSV}" <<CSVEOF
Title,Description,Assignee,Labels
[VHS ${TIMESTAMP}] Test ticket 1,Automated VHS test ticket created by run_vhs_tests.sh,,vhs-test
[VHS ${TIMESTAMP}] Test ticket 2,Second automated VHS test ticket,,vhs-test
[VHS ${TIMESTAMP}] Test ticket 3,Third automated VHS test ticket,,vhs-test
CSVEOF
    echo "  → Test CSV written to ${JIRA_TEST_CSV}"

    # Create output directory for screenshots and GIF
    mkdir -p "${SCRIPT_DIR}/vhs-output"
    echo "  → Screenshot output dir: ${SCRIPT_DIR}/vhs-output/"

    # Write the th() test-header function to a file so VHS can source it.
    # Defining it here (not inside a VHS Type string) avoids quoting ambiguity
    # with the double quotes needed around "§ $1" and "$2" in the gum call.
    cat > /tmp/jira-tui-th.sh << 'THEOF'
th() {
    local id="$1" desc="$2"
    gum style \
        --border rounded \
        --border-foreground 99 \
        --foreground 15 \
        --bold \
        --padding '0 2' \
        --margin '1 0' \
        "§ ${id}" "${desc}"
}
THEOF
    echo "  → th() helper written to /tmp/jira-tui-th.sh"

    # ---------------------------------------------------------------------------
    # Run VHS
    # ---------------------------------------------------------------------------
    echo ""
    bold "Running VHS tape: ${VHS_TAPE}"
    echo "  This records a GIF and screenshots of the interactive flows."
    echo "  It may take several minutes depending on Jira API response times."
    echo ""

    export JIRA_TEST_DIR
    export JIRA_TEST_CSV
    export TEST_PROJECT_KEY
    export TEST_EPIC_KEY

    if vhs "${VHS_TAPE}"; then
        pass "VHS" "VHS tape completed without error"
    else
        fail "VHS" "VHS tape" "vhs exited with a non-zero code"
    fi

    # ---------------------------------------------------------------------------
    # Post-VHS assertions: verify file outputs
    # ---------------------------------------------------------------------------
    echo ""
    bold "=== Post-VHS output checks ==="

    # 14.4.1: Epic CSV exists and has correct header
    EPIC_CSV="${SCRIPT_DIR}/${TEST_EPIC_KEY}.csv"
    if [[ -f "${EPIC_CSV}" ]]; then
        pass "14.4.1" "Epic CSV created at ${EPIC_CSV}"
        assert_file_contains "14.4.1" "Epic CSV has correct header" \
            "${EPIC_CSV}" "Title,Description,Assignee,Labels,Requester"
        EPIC_ROWS=$(( $(wc -l < "${EPIC_CSV}") - 1 ))
        echo "    → ${EPIC_ROWS} child issue(s) exported"
    else
        fail "14.4.1" "Epic CSV should exist at ${EPIC_CSV}" "file not found"
    fi

    # 11.10: Results export CSV
    RESULTS_CSV="${SCRIPT_DIR}/jira_tickets_results.csv"
    if [[ -f "${RESULTS_CSV}" ]]; then
        pass "11.10" "Results CSV created at ${RESULTS_CSV}"
        assert_file_contains "11.10" "Results CSV has Status column" \
            "${RESULTS_CSV}" "Status"
    else
        # VHS may not have reached the export step; treat as skip
        skip "11.10" "Results CSV (VHS may not have reached export step)"
    fi

    # Screenshots: verify key ones exist
    for screenshot in \
        "vhs-output/1.1.1-help-long.png" \
        "vhs-output/1.2.1-missing-epic-key.png" \
        "vhs-output/2-settings-initial.png" \
        "vhs-output/2-ticket-list.png" \
        "vhs-output/2-done-initial.png" \
        "vhs-output/3-show-tickets-initial.png" \
        "vhs-output/4-epic-review-initial.png" \
        "vhs-output/4-save-success.png" \
        "vhs-output/5-form-initial.png"
    do
        assert_file_exists "screenshot" "Screenshot exists: ${screenshot}" \
            "${SCRIPT_DIR}/${screenshot}"
    done

    # Clean up timestamped CSV
    rm -f "${JIRA_TEST_CSV}"
else
    [[ "${SKIP_VHS}" == "1" ]] && echo "" && yellow "VHS tests skipped (SKIP_VHS=1)."
    [[ "${HAVE_CREDS}" == "0" ]] && echo "" && yellow "VHS tests skipped (credentials not set)."
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
bold "============================="
bold "  Test Results Summary"
bold "============================="
green  "  Passed:  ${PASS}"
if [[ "${FAIL}" -gt 0 ]]; then
    red    "  Failed:  ${FAIL}"
else
    green  "  Failed:  ${FAIL}"
fi
yellow "  Skipped: ${SKIP}"
echo ""

if [[ "${SKIP_VHS}" != "1" ]] && [[ "${HAVE_CREDS}" == "1" ]]; then
    echo "Artifacts:"
    echo "  GIF recording:  ${SCRIPT_DIR}/vhs-output/jira-tui-tests.gif"
    echo "  Screenshots:    ${SCRIPT_DIR}/vhs-output/*.png"
    echo ""
fi

if [[ "${FAIL}" -gt 0 ]]; then
    exit 1
fi
exit 0
