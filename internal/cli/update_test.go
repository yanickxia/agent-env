package cli

import "testing"

func TestIsUpdateCommand(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"agent-env", "update"}, true},
		{[]string{"agent-env", "update", "--version", "v0.3.0"}, true},
		{[]string{"agent-env", "--version"}, false},
		{[]string{"agent-env", "version"}, false},
		{[]string{"agent-env", "skills", "update"}, false},
		{[]string{"agent-env", "mcp", "apply"}, false},
		{[]string{"agent-env"}, false},
	}
	for _, tc := range cases {
		if got := isUpdateCommand(tc.args); got != tc.want {
			t.Fatalf("isUpdateCommand(%v) = %v want %v", tc.args, got, tc.want)
		}
	}
}

func TestPrintUpdateNoticeEmptyChannelReturns(t *testing.T) {
	ch := make(chan string, 1)
	// No message: must return promptly without printing.
	printUpdateNotice(ch)
}
