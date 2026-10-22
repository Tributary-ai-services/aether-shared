package workload

import (
	"encoding/json"
	"strings"
	"testing"
)

func bash(cmd string) ToolCall             { return ToolCall{Name: "Bash", Target: cmd} }
func edit(path string) ToolCall            { return ToolCall{Name: "Edit", Target: path} }
func tool(name string) ToolCall            { return ToolCall{Name: name} }
func task(calls ...ToolCall) ActivityInput { return ActivityInput{Calls: calls} }

func TestExtractActivity(t *testing.T) {
	cases := []struct {
		name string
		in   ActivityInput
		want Activity
		conf ActConfidence
	}{
		{"rollout", task(bash("kubectl -n web set image deploy/api api=reg/api:1.2.0"), bash("kubectl rollout status deploy/api")), ActivityDeploy, ConfHigh},
		{"build then push", task(bash("docker build -t reg/api:1.2.0 ."), bash("docker push reg/api:1.2.0")), ActivityBuildImage, ConfHigh},
		{"make docker-build", task(bash("cd svc && make docker-build VERSION=1.2.0")), ActivityBuildImage, ConfHigh},
		{"merge", task(bash("gh pr view 12 --json state"), bash("gh pr merge 12 --squash")), ActivityReviewMerge, ConfHigh},
		{"edit code and test", task(edit("/src/app/server.go"), bash("go test ./...")), ActivityImplement, ConfMed},
		{"heredoc into a code file", task(bash("cat > internal/x/labels.go <<'EOF'\npackage x\nEOF")), ActivityImplement, ConfMed},
		{"tests only", task(bash("go test -race ./internal/...")), ActivityTestVerify, ConfMed},
		{"lint only", task(bash("node --check page.js")), ActivityTestVerify, ConfMed},
		{"docs edit", task(edit("/repo/docs/guide.md")), ActivityDocument, ConfMed},
		{"artifact publish", task(tool("Artifact")), ActivityDocument, ConfMed},
		{"append to a doc", task(bash("cat >> notes/runbook.md <<'EOF'\n- step\nEOF")), ActivityDocument, ConfMed},
		{"bookkeeping edit", ActivityInput{Calls: []ToolCall{edit("/root/BACKLOG.md")}, Bookkeeping: []string{"BACKLOG.md"}}, ActivityDocument, ConfMed},
		{"commit only", task(bash("git add -A && git commit -m 'x'"), bash("git push -u origin feat/x")), ActivityHygiene, ConfLow},
		{"open PR only", task(bash("gh pr create --title t --body b")), ActivityHygiene, ConfLow},
		{"pr view only", task(bash("gh pr checks 12")), ActivityReviewMerge, ConfLow},
		{"read-only code", task(tool("Read"), bash("grep -rn foo src/ | head")), "", ConfLow},
		{"read-only cluster", task(bash("kubectl get pods -n web"), bash("kubectl logs deploy/api --tail=50")), "", ConfLow},
		{"scratch write only", task(bash("cat > /tmp/x/check.py <<'EOF'\nprint(1)\nEOF"), edit("/tmp/x/scratchpad/a.go")), "", ConfLow},
		{"no calls", task(), "", ConfLow},
		{"subagent", task(tool("Agent")), "", ConfLow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ExtractActivity(c.in)
			if r.Activity != c.want || r.Confidence != c.conf {
				t.Fatalf("got %q/%s want %q/%s (verbs %v)", r.Activity, r.Confidence, c.want, c.conf, r.Verbs)
			}
			if r.Version != ActivityExtractorVersion {
				t.Fatalf("version %q", r.Version)
			}
		})
	}
}

// The order is the decision: a task that both edits code and rolls the cluster is a deploy, and one that
// builds an image after editing is build_image.
func TestActivityPrecedence(t *testing.T) {
	r := ExtractActivity(task(edit("/src/main.go"), bash("docker build -t a ."), bash("kubectl apply -f k8s/")))
	if r.Activity != ActivityDeploy {
		t.Fatalf("got %q", r.Activity)
	}
	r = ExtractActivity(task(edit("/src/main.go"), bash("docker build -t a .")))
	if r.Activity != ActivityBuildImage {
		t.Fatalf("got %q", r.Activity)
	}
}

func TestShellWrites(t *testing.T) {
	cases := []struct {
		cmd  string
		want Verb // "" = no write counted
	}{
		{"grep foo bar.go 2>/dev/null", ""},
		{"ls > /tmp/out.txt", ""},
		{"cmd 2>&1 | tail -3", ""},
		{"echo hi > README.md", VerbEditDoc},
		{"echo x | tee -a config.yaml", VerbEditCode},
		{"sed -i 's/a/b/' main.go", VerbFileWrite},
		{"cp a b", VerbFileWrite},
		{`echo 1 > "$OUT"`, VerbFileWrite},
		{"grep x f > /dev/null; echo y > Makefile", VerbEditCode},
	}
	for _, c := range cases {
		v := map[Verb]int{}
		countShellWrites(v, c.cmd, nil)
		got := Verb("")
		for _, k := range []Verb{VerbEditDoc, VerbEditCode, VerbFileWrite, VerbBookkeeping} {
			if v[k] > 0 {
				got = k
			}
		}
		if got != c.want {
			t.Errorf("%q: got %q want %q (%v)", c.cmd, got, c.want, v)
		}
	}
}

func TestStripHeredocs(t *testing.T) {
	in := "git commit -F - <<'EOF'\nkubectl apply -f x\ndocker push y\nEOF\necho done"
	out := stripHeredocs(in)
	if strings.Contains(out, "kubectl") || strings.Contains(out, "docker") || !strings.Contains(out, "echo done") || !strings.Contains(out, "git commit") {
		t.Fatalf("got %q", out)
	}
	// A commit message that mentions a deploy must not make the task a deploy.
	r := ExtractActivity(task(bash(in)))
	if r.Activity != ActivityHygiene {
		t.Fatalf("got %q (verbs %v)", r.Activity, r.Verbs)
	}
	// Unterminated heredoc: the body runs to the end and is dropped.
	if out := stripHeredocs("python3 - <<EOF\nimport os\nos.system('kubectl delete x')"); strings.Contains(out, "kubectl") {
		t.Fatalf("got %q", out)
	}
}

// The result holds no text: only the activity, a confidence, a version and enum-keyed counts.
func TestActivityResultCarriesNoText(t *testing.T) {
	secret := "/home/alice/acme-secret-project/payroll.go"
	r := ExtractActivity(task(edit(secret), bash("curl -H 'Authorization: Bearer sk-live-123' https://acme.internal/api")))
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"alice", "acme", "payroll", "sk-live", "Bearer"} {
		if strings.Contains(string(b), w) {
			t.Fatalf("result contains %q: %s", w, b)
		}
	}
	known := map[Verb]bool{}
	for _, vr := range verbRules {
		known[vr.v] = true
	}
	for _, k := range []Verb{VerbFileWrite, VerbEditCode, VerbEditDoc, VerbBookkeeping, VerbDelegate} {
		known[k] = true
	}
	for k := range r.Verbs {
		if !known[k] {
			t.Fatalf("verb key %q is not in the enum", k)
		}
	}
}

func BenchmarkExtractActivity(b *testing.B) {
	in := task(
		bash("cd /src && git status && go test ./... 2>&1 | tail -5"),
		edit("/src/internal/a/b.go"),
		bash("cat > /src/cmd/x/main.go <<'EOF'\n"+strings.Repeat("fmt.Println(1)\n", 200)+"EOF"),
		bash("docker build -t reg/x:1.0.0 . && docker push reg/x:1.0.0"),
		bash("kubectl -n x set image deploy/x x=reg/x:1.0.0 && kubectl rollout status deploy/x"),
	)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ExtractActivity(in)
	}
}
