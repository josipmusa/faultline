package tlsmitm

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// lookPathWith returns a PATH lookup that only knows the given tools.
func lookPathWith(tools ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, t := range tools {
			if t == name {
				return "/usr/sbin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestTrustPlanMacOSUsesSecurity(t *testing.T) {
	plan := TrustPlan("darwin", lookPathWith(), "/c/ca.crt")

	want := [][]string{{"sudo", "security", "add-trusted-cert", "-d", "-r", "trustRoot",
		"-k", "/Library/Keychains/System.keychain", "/c/ca.crt"}}
	if !reflect.DeepEqual(plan.Commands, want) {
		t.Errorf("Commands = %v, want %v", plan.Commands, want)
	}
	if !strings.Contains(plan.Instructions, "Keychain") {
		t.Errorf("instructions do not mention the keychain:\n%s", plan.Instructions)
	}
}

func TestTrustPlanQuotesPathsWithSpacesInInstructionsOnly(t *testing.T) {
	path := "/Users/x/Library/Application Support/faultline/ca.crt"
	plan := TrustPlan("darwin", lookPathWith(), path)

	if !strings.Contains(plan.Instructions, `"`+path+`"`) {
		t.Errorf("instructions do not quote the path:\n%s", plan.Instructions)
	}
	if got := plan.Commands[0][len(plan.Commands[0])-1]; got != path {
		t.Errorf("argv path = %q, want the unquoted %q", got, path)
	}
}

func TestTrustPlanLinuxPicksTheInstalledTooling(t *testing.T) {
	cases := []struct {
		tool string
		want [][]string
	}{
		{"update-ca-certificates", [][]string{
			{"sudo", "cp", "/c/ca.crt", "/usr/local/share/ca-certificates/faultline.crt"},
			{"sudo", "update-ca-certificates"},
		}},
		{"update-ca-trust", [][]string{
			{"sudo", "cp", "/c/ca.crt", "/etc/pki/ca-trust/source/anchors/faultline.crt"},
			{"sudo", "update-ca-trust"},
		}},
		{"trust", [][]string{
			{"sudo", "trust", "anchor", "--store", "/c/ca.crt"},
		}},
	}
	for _, tc := range cases {
		plan := TrustPlan("linux", lookPathWith(tc.tool), "/c/ca.crt")
		if !reflect.DeepEqual(plan.Commands, tc.want) {
			t.Errorf("%s: Commands = %v, want %v", tc.tool, plan.Commands, tc.want)
		}
	}
}

func TestTrustPlanLinuxWithoutToolingOnlyExplains(t *testing.T) {
	plan := TrustPlan("linux", lookPathWith(), "/c/ca.crt")
	if plan.Commands != nil {
		t.Errorf("Commands = %v, want none", plan.Commands)
	}
	if !strings.Contains(plan.Instructions, "/c/ca.crt") {
		t.Errorf("instructions do not name the certificate:\n%s", plan.Instructions)
	}
}

func TestTrustPlanWindowsIsInstructionsOnly(t *testing.T) {
	plan := TrustPlan("windows", lookPathWith("certutil"), `C:\c\ca.crt`)
	if plan.Commands != nil {
		t.Errorf("Commands = %v, want none", plan.Commands)
	}
	if !strings.Contains(plan.Instructions, "certutil") {
		t.Errorf("instructions do not mention certutil:\n%s", plan.Instructions)
	}
}

// recordingRunner remembers every command it was asked to run.
type recordingRunner struct{ ran [][]string }

func (r *recordingRunner) run(_ context.Context, argv []string) error {
	r.ran = append(r.ran, argv)
	return nil
}

func TestInstallRunsCommandsOnlyAfterYes(t *testing.T) {
	plan := Plan{Instructions: "do it", Commands: [][]string{{"a"}, {"b", "c"}}}

	for _, tc := range []struct {
		answer string
		want   [][]string
	}{
		{"y\n", plan.Commands},
		{"yes\n", plan.Commands},
		{"n\n", nil},
		{"\n", nil},
		{"", nil}, // stdin closed: never assume consent
	} {
		var r recordingRunner
		var out strings.Builder
		err := Install(context.Background(), plan, strings.NewReader(tc.answer), &out, r.run)
		if err != nil {
			t.Fatalf("answer %q: Install: %v", tc.answer, err)
		}
		if !reflect.DeepEqual(r.ran, tc.want) {
			t.Errorf("answer %q: ran %v, want %v", tc.answer, r.ran, tc.want)
		}
		if !strings.Contains(out.String(), "do it") {
			t.Errorf("answer %q: instructions were not printed:\n%s", tc.answer, out.String())
		}
	}
}

func TestInstallWithoutCommandsDoesNotPrompt(t *testing.T) {
	var r recordingRunner
	var out strings.Builder
	err := Install(context.Background(), Plan{Instructions: "by hand"}, strings.NewReader("y\n"), &out, r.run)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if r.ran != nil {
		t.Errorf("ran %v, want nothing", r.ran)
	}
	if strings.Contains(out.String(), "[y/N]") {
		t.Errorf("prompted although there is nothing to run:\n%s", out.String())
	}
}

func TestInstallStopsAtFirstFailingCommand(t *testing.T) {
	plan := Plan{Commands: [][]string{{"a"}, {"b"}}}
	var ran [][]string
	run := func(_ context.Context, argv []string) error {
		ran = append(ran, argv)
		return errors.New("boom")
	}
	err := Install(context.Background(), plan, strings.NewReader("y\n"), &strings.Builder{}, run)
	if err == nil || !strings.Contains(err.Error(), "a") {
		t.Fatalf("Install error = %v, want one naming the command", err)
	}
	if len(ran) != 1 {
		t.Errorf("ran %v, want only the first command", ran)
	}
}

func TestTrustPlanUnknownOSNamesTheCertificate(t *testing.T) {
	plan := TrustPlan("plan9", lookPathWith(), "/c/ca.crt")
	if plan.Commands != nil {
		t.Errorf("Commands = %v, want none", plan.Commands)
	}
	if !strings.Contains(plan.Instructions, "/c/ca.crt") || !strings.Contains(plan.Instructions, "plan9") {
		t.Errorf("instructions should name the OS and the certificate:\n%s", plan.Instructions)
	}
}
