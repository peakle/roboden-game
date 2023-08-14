package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	var args arguments
	flag.StringVar(&args.output, "o", "",
		"the output binary name")
	flag.StringVar(&args.commit, "commit", "",
		"a commit hash")
	flag.StringVar(&args.goos, "goos", "",
		"select a cross-compilation GOOS value")
	flag.StringVar(&args.goarch, "goarch", "",
		"select a cross-compilation GOARCH value")
	flag.BoolVar(&args.steam, "steam", false,
		"whether we're building for Steam")
	flag.Parse()

	commit := args.commit
	if commit == "" {
		out, err := exec.Command("git", "rev-parse", "HEAD").CombinedOutput()
		if err != nil {
			panic(err)
		}
		commit = strings.TrimSpace(string(out))
	}

	ldFlags := []string{
		fmt.Sprintf("-X 'main.CommitHash=%s'", commit),
		"-X 'main.DefaultServerAddr=https://quasilyte.tech/roboden/api'",
		"-s -w",
	}
	buildTags := []string{}
	if args.steam {
		buildTags = append(buildTags, "steam")
	}
	goFlags := []string{
		"build",
		"--ldflags", strings.Join(ldFlags, " "),
		"--tags", strings.Join(buildTags, " "),
		"--trimpath",
		"-v",
		"-o", args.output,
		"./cmd/game",
	}
	cmd := exec.Command("go", goFlags...)
	cmd.Env = append([]string{}, os.Environ()...) // Copy env slice
	if args.goos != "" {
		cmd.Env = append(cmd.Env, "GOOS="+args.goos)
	}
	if args.goarch != "" {
		cmd.Env = append(cmd.Env, "GOARCH="+args.goarch)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("%v: %s", err, out))
	}
}

type arguments struct {
	commit string
	output string

	goos   string
	goarch string

	steam bool
}
