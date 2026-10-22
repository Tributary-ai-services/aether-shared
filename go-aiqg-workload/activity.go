package workload

import (
	"path"
	"regexp"
	"strings"
)

// Coarse activity: which SDLC step one agent task performed, read from the tools it called.
//
// This is the settled vantage (mode B): it runs after the response, over the calls of one task that
// the caller has already segmented. It reads each call's name and target (a command, a path) to count
// verbs from a fixed enum, then picks an activity by an ordered table. Targets are READ and never
// retained: an ActivityResult holds the activity, a confidence and verb counts, nothing else.
//
// Structure recovers the steps that leave a mechanical trace (building an image, rolling a cluster,
// merging, running tests, editing code or docs). It does not recover purpose: design, triage,
// review_plan, requirements and discuss read the same files and run the same greps as everything
// else, so the table abstains on them (Activity == "") and leaves them to the semantic tier. A
// wrong activity is worse than none, for the same reason a wrong ToolFamily is.

// ActivityExtractorVersion stamps ActivityResult. Bump it when a rule's meaning changes.
const ActivityExtractorVersion = "ca-1"

// Activity is an SDLC step. The values are the labeling skill's (tas-session-corpus
// skills/sdlc-turn-labeling, section 1); this extractor emits only the structural subset.
type Activity string

const (
	ActivityRequirements Activity = "requirements"
	ActivityDesign       Activity = "design"
	ActivityImplement    Activity = "implement"
	ActivityTestVerify   Activity = "test_verify"
	ActivityBuildImage   Activity = "build_image"
	ActivityReviewMerge  Activity = "review_merge"
	ActivityDeploy       Activity = "deploy_release"
	ActivityOperate      Activity = "operate_diagnose"
	ActivityHygiene      Activity = "maintain_hygiene"
	ActivityDocument     Activity = "document"
	ActivityTriage       Activity = "triage"
	ActivityReviewPlan   Activity = "review_plan"
	ActivityDiscuss      Activity = "discuss"
)

// Verb is one bucket of the command-verb enum. Commands are bucketed and the text is dropped.
type Verb string

const (
	VerbDockerBuild Verb = "docker_build" // docker build / buildx, make docker-build
	VerbDockerPush  Verb = "docker_push"  // docker push / tag, make docker-push
	VerbK8sMutate   Verb = "k8s_mutate"   // kubectl apply/set image/rollout restart/scale/patch/delete, helm upgrade
	VerbK8sRead     Verb = "k8s_read"     // kubectl get/describe/logs/top/exec/wait, rollout status
	VerbGitCommit   Verb = "git_commit"
	VerbGitPush     Verb = "git_push"
	VerbGitBranch   Verb = "git_branch" // branch, worktree, checkout, switch, stash, reset, rebase, pull, merge, prune
	VerbGitRead     Verb = "git_read"   // status, diff, log, show, fetch, blame
	VerbPRCreate    Verb = "pr_create"
	VerbPRMerge     Verb = "pr_merge"    // gh pr merge
	VerbPRReview    Verb = "pr_review"   // gh pr view/diff/checks/review/comment
	VerbGHOther     Verb = "gh_other"    // gh issue/run/api/release/...
	VerbTest        Verb = "test"        // go test, pytest, npm test, make test, vitest, jest
	VerbLint        Verb = "lint"        // go vet, golangci-lint, eslint, node --check, tsc --noEmit
	VerbCompile     Verb = "compile"     // go build, make build, npm run build, tsc
	VerbHTTP        Verb = "http"        // curl, wget
	VerbDB          Verb = "db"          // psql, cypher-shell, redis-cli, mc, kafka-*
	VerbRemote      Verb = "remote"      // ssh, scp, systemctl, journalctl
	VerbFileWrite   Verb = "file_write"  // a shell write whose target is not a known doc or code path (sed -i, cp, mv, > "$VAR")
	VerbEditCode    Verb = "edit_code"   // an edit tool on a non-doc path, or a shell write to a code or config path
	VerbEditDoc     Verb = "edit_doc"    // an edit or shell write to a doc path, or a document/artifact tool
	VerbBookkeeping Verb = "bookkeeping" // an edit to a path on the tenant's bookkeeping list (backlog, memory)
	VerbDelegate    Verb = "delegate"    // a subagent
)

// ActivityInput is one task's calls. Bookkeeping lists path fragments the tenant treats as standing
// bookkeeping (a backlog file, a memory directory); an edit there is hygiene, not implementation. It is
// tenant config, so it is passed in, never built in.
type ActivityInput struct {
	Calls       []ToolCall
	Bookkeeping []string
}

// ActivityResult holds the activity (empty when the table abstains), a confidence and verb counts.
type ActivityResult struct {
	Version    string
	Activity   Activity
	Confidence ActConfidence
	Verbs      map[Verb]int
}

type verbRule struct {
	v  Verb
	rx *regexp.Regexp
}

// Order does not matter here: every rule that matches counts. One command can carry several verbs
// ("git commit && git push").
var verbRules = []verbRule{
	{VerbDockerBuild, regexp.MustCompile(`\bdocker\s+(buildx\s+)?build\b|\bmake\s+(-\S+\s+)*docker-build\S*`)},
	{VerbDockerPush, regexp.MustCompile(`\bdocker\s+(push|tag)\b|\bmake\s+(-\S+\s+)*docker-push\S*`)},
	{VerbK8sMutate, regexp.MustCompile(`\bkubectl\s+(-\S+\s+(\S+\s+)?)*(apply|set\s+image|rollout\s+(restart|undo)|scale|patch|delete|replace|create|label|annotate)\b|\bhelm\s+(upgrade|install|rollback|uninstall)\b`)},
	{VerbK8sRead, regexp.MustCompile(`\bkubectl\s+(-\S+\s+(\S+\s+)?)*(get|describe|logs|top|exec|port-forward|wait|rollout\s+(status|history)|auth|events)\b`)},
	{VerbGitCommit, regexp.MustCompile(`\bgit\s+(-C\s+\S+\s+)?commit\b`)},
	{VerbGitPush, regexp.MustCompile(`\bgit\s+(-C\s+\S+\s+)?push\b`)},
	{VerbGitBranch, regexp.MustCompile(`\bgit\s+(-C\s+\S+\s+)?(branch|worktree|checkout|switch|stash|reset|restore|rebase|pull|merge|cherry-pick|prune|gc|clean|remote)\b`)},
	{VerbGitRead, regexp.MustCompile(`\bgit\s+(-C\s+\S+\s+)?(status|diff|log|show|fetch|blame|ls-files|ls-tree|rev-parse|describe)\b`)},
	{VerbPRCreate, regexp.MustCompile(`\bgh\s+pr\s+create\b`)},
	{VerbPRMerge, regexp.MustCompile(`\bgh\s+pr\s+merge\b`)},
	{VerbPRReview, regexp.MustCompile(`\bgh\s+pr\s+(view|diff|checks|review|comment|list|status)\b`)},
	{VerbGHOther, regexp.MustCompile(`\bgh\s+(issue|run|api|repo|workflow|release|search)\b`)},
	{VerbTest, regexp.MustCompile(`\bgo\s+test\b|\bpytest\b|\bnpm\s+(run\s+)?test\b|\bmake\s+(-\S+\s+)*(test|ci|check)\b|\bvitest\b|\bjest\b|\bcargo\s+test\b`)},
	{VerbLint, regexp.MustCompile(`\bgo\s+vet\b|\bgolangci-lint\b|\bgofmt\b|\beslint\b|\bruff\b|\bnode\s+--check\b|\btsc\s+--noEmit\b|\bnpm\s+run\s+lint\b|\bmake\s+lint\b`)},
	{VerbCompile, regexp.MustCompile(`\bgo\s+build\b|\bmake\s+(-\S+\s+)*build\b|\bnpm\s+run\s+build\b|\bcargo\s+build\b`)},
	{VerbHTTP, regexp.MustCompile(`\b(curl|wget)\b`)},
	{VerbDB, regexp.MustCompile(`\b(psql|cypher-shell|redis-cli|kafka-[a-z-]+|mongosh)\b|\bmc\s+(ls|cp|cat|stat|mb|rm)\b`)},
	{VerbRemote, regexp.MustCompile(`\b(ssh|scp|systemctl|journalctl)\b`)},
}

// scratchPath marks a write that leaves no lasting change: temp dirs, scratchpads, /dev/null.
var scratchPath = regexp.MustCompile(`/tmp/|/dev/null|scratchpad|/var/folders/`)

// File writes are judged per shell segment, so a "2>/dev/null" in one segment does not hide a real
// write in another. A segment counts when it writes somewhere that is not a scratch path.
var (
	segmentSplit  = regexp.MustCompile(`&&|\|\||;|\|`)
	reRedirect    = regexp.MustCompile(`(?:^|[^0-9&>])>>?\s*([^\s;&|)>]+)`)
	reTee         = regexp.MustCompile(`\btee\s+(?:-a\s+)?([^\s;&|)]+)`)
	reInPlaceEdit = regexp.MustCompile(`\b(sed|perl)\s+(-\S*\s+)*-i\b|\b(cp|mv|mkdir|touch|rm|ln)\s`)
	heredocStart  = regexp.MustCompile(`<<-?\s*['"]?(\w+)['"]?`)
)

var codeExt = map[string]bool{
	".go": true, ".py": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".sh": true, ".bash": true,
	".yaml": true, ".yml": true, ".json": true, ".toml": true, ".sql": true, ".cypher": true, ".rs": true, ".java": true,
	".css": true, ".scss": true, ".tf": true, ".mod": true, ".proto": true, ".rb": true, ".c": true, ".h": true, ".cpp": true,
}

var docExt = map[string]bool{".md": true, ".mdx": true, ".rst": true, ".adoc": true, ".txt": true, ".html": true, ".docx": true}

// ExtractActivity counts verbs over one task's calls and picks an activity by an ordered table.
func ExtractActivity(in ActivityInput) ActivityResult {
	r := ActivityResult{Version: ActivityExtractorVersion, Verbs: map[Verb]int{}}
	for _, c := range in.Calls {
		countCall(r.Verbs, c, in.Bookkeeping)
	}
	r.Activity, r.Confidence = decideActivity(r.Verbs)
	return r
}

func countCall(v map[Verb]int, c ToolCall, bookkeeping []string) {
	if isShellTool(c.Name) {
		cmd := stripHeredocs(c.Target)
		for _, vr := range verbRules {
			if vr.rx.MatchString(cmd) {
				v[vr.v]++
			}
		}
		countShellWrites(v, cmd, bookkeeping)
		return
	}
	toks := tokenize(c.Name)
	if hasToken(toks, "artifact", "docs", "doc", "document") && !hasToken(toks, "read", "list", "guide", "query", "export") {
		v[VerbEditDoc]++
		return
	}
	switch FamilyOf(c.Name) {
	case FamilyEdit:
		if k := pathVerb(c.Target, bookkeeping); k != "" {
			v[k]++
		} else if !scratchPath.MatchString(c.Target) {
			v[VerbEditCode]++ // an edit tool on an unrecognised path is still a change
		}
	case FamilyDelegate:
		v[VerbDelegate]++
	}
}

// decideActivity is the table. The order is the decision: earlier rules win. Each rule names the
// trace that is hard to leave by accident, so the strongest mechanical evidence decides.
func decideActivity(v map[Verb]int) (Activity, ActConfidence) {
	switch {
	// Rolling a cluster is the most specific trace there is.
	case v[VerbK8sMutate] > 0:
		return ActivityDeploy, ConfHigh
	// An image built or pushed and nothing rolled: build_image (the skill splits build then deploy).
	case v[VerbDockerBuild] > 0 || v[VerbDockerPush] > 0:
		return ActivityBuildImage, ConfHigh
	case v[VerbPRMerge] > 0:
		return ActivityReviewMerge, ConfHigh
	// Changed code: implement, even when tests ran in the same task.
	case v[VerbEditCode] > 0:
		return ActivityImplement, ConfMed
	case v[VerbTest] > 0 || v[VerbLint] > 0 || v[VerbCompile] > 0:
		return ActivityTestVerify, ConfMed
	// Backlog and memory edits are labeled document, not hygiene: the bookkeeping list feeds
	// origin=standing_rule, it does not change the step.
	case v[VerbEditDoc] > 0 || v[VerbBookkeeping] > 0:
		return ActivityDocument, ConfMed
	// Committing, pushing or opening a PR with nothing edited in this task: the change was made in an
	// earlier task, and this one files it.
	case v[VerbGitCommit] > 0 || v[VerbGitPush] > 0 || v[VerbPRCreate] > 0:
		return ActivityHygiene, ConfLow
	case v[VerbPRReview] > 0:
		return ActivityReviewMerge, ConfLow
	}
	// Read-only work (greps, kubectl get, logs, queries) abstains. On a live system it is diagnosis
	// or the check after a deploy, and only purpose tells them apart; on code it is triage, design
	// or groundwork for the next task.
	return "", ConfLow
}

func isShellTool(name string) bool {
	return hasToken(tokenize(name), "bash", "shell", "exec", "terminal", "cmd", "command", "sh")
}

func hasToken(toks []string, want ...string) bool {
	for _, t := range toks {
		for _, w := range want {
			if t == w {
				return true
			}
		}
	}
	return false
}

func onList(p string, list []string) bool {
	for _, f := range list {
		if f != "" && strings.Contains(p, f) {
			return true
		}
	}
	return false
}

// countShellWrites counts each write target in a command: doc, code, bookkeeping, or unknown.
func countShellWrites(v map[Verb]int, cmd string, bookkeeping []string) {
	for _, seg := range segmentSplit.Split(cmd, -1) {
		var targets []string
		for _, m := range reRedirect.FindAllStringSubmatch(seg, -1) {
			targets = append(targets, m[1])
		}
		if m := reTee.FindStringSubmatch(seg); m != nil {
			targets = append(targets, m[1])
		}
		for _, t := range targets {
			if strings.HasPrefix(t, "&") || scratchPath.MatchString(t) {
				continue
			}
			if k := pathVerb(t, bookkeeping); k != "" {
				v[k]++
			} else {
				v[VerbFileWrite]++
			}
		}
		if len(targets) == 0 && reInPlaceEdit.MatchString(seg) && !scratchPath.MatchString(seg) {
			v[VerbFileWrite]++
		}
	}
}

// pathVerb buckets a written path: bookkeeping, doc, code, or "" when the path says nothing.
func pathVerb(p string, bookkeeping []string) Verb {
	p = strings.Trim(p, `"'`)
	switch {
	case p == "" || scratchPath.MatchString(p):
		return ""
	case onList(p, bookkeeping):
		return VerbBookkeeping
	}
	base := strings.ToLower(path.Base(p))
	ext := path.Ext(base)
	switch {
	case docExt[ext]:
		return VerbEditDoc
	case codeExt[ext] || base == "makefile" || base == "dockerfile" || strings.HasPrefix(base, "dockerfile."):
		return VerbEditCode
	}
	return ""
}

// stripHeredocs drops each heredoc body (a script, a commit message: data, not the command), keeping
// the line that opened it. RE2 has no backreferences, so the terminator is found by hand.
func stripHeredocs(cmd string) string {
	for {
		loc := heredocStart.FindStringSubmatchIndex(cmd)
		if loc == nil {
			return cmd
		}
		word := cmd[loc[2]:loc[3]]
		nl := strings.IndexByte(cmd[loc[1]:], '\n')
		if nl < 0 {
			return cmd[:loc[0]] + cmd[loc[1]:]
		}
		bodyStart := loc[1] + nl + 1
		end := len(cmd)
		rest := cmd[bodyStart:]
		for off := 0; off < len(rest); {
			lineEnd := strings.IndexByte(rest[off:], '\n')
			line := rest[off:]
			if lineEnd >= 0 {
				line = rest[off : off+lineEnd]
			}
			if strings.TrimSpace(line) == word {
				end = bodyStart + off + len(line)
				break
			}
			if lineEnd < 0 {
				break
			}
			off += lineEnd + 1
		}
		cmd = cmd[:loc[0]] + cmd[loc[1]:bodyStart] + cmd[end:]
	}
}
