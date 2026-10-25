package wiki

import (
	"fmt"
	"strings"
)

const reviewUsage = `usage:
  company-wiki review prepare <batch> <topic> --card card.json --draft-dir draft/
  company-wiki review preview <batch> <topic> [topic ...]
  company-wiki review decide <batch> <topic> --revision HASH --decision approved|changes_requested|deferred|rejected --response response.json
  company-wiki review apply <batch> <topic> [topic ...]
  company-wiki review status [batch]
  company-wiki review recover`

// ReviewCmd runs `company-wiki review`.
func ReviewCmd(c *Config, args []string, x IO) int {
	if len(args) == 0 {
		fmt.Fprintln(x.Stderr, reviewUsage)
		return 2
	}
	sub := args[0]
	opts, pos, err := parseFlags(args[1:], map[string]bool{"card": true, "draft-dir": true, "revision": true, "decision": true, "response": true}, nil)
	if err != nil {
		fmt.Fprintf(x.Stderr, "%v\n%s\n", err, reviewUsage)
		return 2
	}
	need := func(n int, names ...string) bool {
		if len(pos) < n {
			return false
		}
		for _, name := range names {
			if opts[name] == "" {
				return false
			}
		}
		return true
	}
	r, err := NewReview(c)
	if err != nil {
		fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
		return 1
	}
	var result any
	switch sub {
	case "prepare":
		if !need(2, "card", "draft-dir") || len(pos) != 2 {
			break
		}
		card, e := readJSON(resolvePath(opts["card"], x.Dir))
		if e != nil {
			err = e
		} else {
			result, err = r.Prepare(pos[0], pos[1], card, resolvePath(opts["draft-dir"], x.Dir))
		}
	case "decide":
		if !need(2, "revision", "decision", "response") || len(pos) != 2 {
			break
		}
		if !decisions[opts["decision"]] {
			fmt.Fprintln(x.Stderr, "--decision must be approved, changes_requested, deferred or rejected")
			return 2
		}
		response, e := readJSON(resolvePath(opts["response"], x.Dir))
		if e != nil {
			err = e
		} else {
			result, err = r.Decide(pos[0], pos[1], opts["revision"], opts["decision"], response)
		}
	case "preview":
		if need(2) {
			result, err = r.Preview(pos[0], pos[1:])
		}
	case "apply":
		if need(2) {
			result, err = r.Apply(pos[0], pos[1:])
		}
	case "status":
		switch len(pos) {
		case 0:
			result, err = r.StatusAll()
		case 1:
			result, err = r.Status(pos[0])
		}
	case "recover":
		if len(pos) == 0 {
			result, err = r.Recover()
		}
	}
	if result == nil && err == nil {
		fmt.Fprintln(x.Stderr, reviewUsage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
		return 1
	}
	if s, ok := result.(string); ok {
		fmt.Fprintln(x.Stdout, s)
		return 0
	}
	if o, ok := result.(*omap); ok {
		fmt.Fprintln(x.Stdout, pyJSONIndent(o))
		return 0
	}
	data, _ := jsonIndent(result)
	fmt.Fprint(x.Stdout, strings.TrimRight(string(data), "\n")+"\n")
	return 0
}
