package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/ui"
)

// TestTheInstanceLineIsJudgedWithItsOwnRefusal drives `resolveInstance`: unwritten and written-valid
// are ADMITTED (half the point — a resolver refusing every line would satisfy every refusal), a
// blank line and each shape refusal name the flag.
func TestTheInstanceLineIsJudgedWithItsOwnRefusal(t *testing.T) {
	instanceLine := func(v string, written bool) appLine {
		return line(flagInstanceName, EnvUIInstanceName, v, written)
	}
	for _, tc := range []struct {
		name string
		l    appLine
		want string // "" = admitted
		got  string
	}{
		{"unwritten is unlabelled", instanceLine("", false), "", ""},
		{"a label", instanceLine("acme-staging", true), "", "acme-staging"},
		{"an empty flag", instanceLine("", true), "reduces to nothing", ""},
		{"a whitespace value", instanceLine("   ", true), "reduces to nothing", ""},
		{"a zero-width value", instanceLine("\u200b", true), "reduces to nothing", ""},
		{"a newline", instanceLine("acme\nprod", true), "-instance-name / $CAIRN_UI_INSTANCE_NAME: ui: the instance name", ""},
		{"too long", instanceLine(strings.Repeat("x", ui.InstanceNameMax+1), true), "at most 32 are accepted", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveInstance(tc.l)
			if tc.want == "" {
				if err != nil || got != tc.got {
					t.Fatalf("resolveInstance = %q, %v; want %q, nil", got, err, tc.got)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("resolveInstance = %q, %v; want a refusal carrying %q", got, err, tc.want)
			}
		})
	}
}

// TestTheBinaryLabelsItsPagesAndSaysSo drives `main` end to end: the label reaches the startup line
// AND a served page's title — the wiring `internal/ui`'s tests cannot see, since each hands the
// server an `App` itself. The unlabelled arm is the control: the same page, today's title.
func TestTheBinaryLabelsItsPagesAndSaysSo(t *testing.T) {
	for _, arm := range []struct {
		name      string
		env, args []string
		line      string
		title     string
	}{
		{"a flag", nil, []string{"-instance-name", "acme-staging"}, `instance "acme-staging"`,
			"<title>acme-staging — sign in · cairn</title>"},
		{"the variable", []string{EnvUIInstanceName + "=acme-staging"}, nil, `instance "acme-staging"`,
			"<title>acme-staging — sign in · cairn</title>"},
		{"unset", nil, nil, "instance unlabelled (no -instance-name)", "<title>cairn — sign in</title>"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			c := startAppChild(t, arm.env, append([]string{"-port", "0"}, arm.args...)...)
			base := c.serving(t)
			if !strings.Contains(c.out.String(), arm.line) {
				t.Errorf("the startup line does not say %q:\n%s", arm.line, c.out.String())
			}
			r, err := (&http.Client{Timeout: 10 * time.Second}).Get(base + ui.SignInPath)
			if err != nil {
				t.Fatalf("GET %s: %v", ui.SignInPath, err)
			}
			defer r.Body.Close()
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), arm.title) {
				t.Errorf("the served sign-in page does not carry %s:\n%s", arm.title, body)
			}
		})
	}
	// And a refused label stops the binary with its own message.
	c := startAppChild(t, []string{EnvUIInstanceName + "=  "}, "-port", "0")
	if code := c.exit(t); code != exitConfig || !strings.Contains(c.out.String(), "$CAIRN_UI_INSTANCE_NAME is set to") {
		t.Fatalf("a blank instance variable: exit %d\n%s", code, c.out.String())
	}
}
