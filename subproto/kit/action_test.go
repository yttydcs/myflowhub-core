package kit

import "testing"

func TestKindFromName(t *testing.T) {
	cases := []struct {
		name string
		want ActionKind
	}{
		{name: "", want: ActionKindUnknown},
		{name: "set", want: ActionKindLocal},
		{name: "assist_set", want: ActionKindAssist},
		{name: "up_login", want: ActionKindUp},
		{name: "notify_set", want: ActionKindNotify},
		{name: "  NOTIFY_x  ", want: ActionKindNotify},
	}
	for _, tc := range cases {
		got := KindFromName(tc.name)
		if got != tc.want {
			t.Fatalf("KindFromName(%q)=%v, want=%v", tc.name, got, tc.want)
		}
	}
}

