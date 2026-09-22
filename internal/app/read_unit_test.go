package app

import "testing"

func TestFormatRead(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		offset int
		want   string
	}{
		{
			name:   "truncated without total",
			raw:    `{"content":"b\nc\n","next_offset":4,"truncated":true}`,
			offset: 2,
			want:   "2|b\n3|c\n… lines 2–3; read offset=4 for more\n",
		},
		{
			name:   "full from start",
			raw:    `{"content":"a\nb\n","total_lines":2}`,
			offset: 1,
			want:   "1|a\n2|b\n",
		},
		{
			name:   "full from offset",
			raw:    `{"content":"b\nc\n","total_lines":3}`,
			offset: 2,
			want:   "2|b\n3|c\n… lines 2–3 of 3\n",
		},
		{
			name:   "plain fallback",
			raw:    "x\ny",
			offset: 1,
			want:   "1|x\n2|y\n",
		},
		{
			name:   "empty",
			raw:    `{"content":""}`,
			offset: 1,
			want:   "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatRead(c.raw, c.offset); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
