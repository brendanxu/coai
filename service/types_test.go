package service

import "testing"

func TestFormatPriceCNY(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, "免费"},
		{-1, "免费"},
		{100, "¥1"},
		{1900, "¥19"},     // diy_agent tier
		{29900, "¥299"},   // content_pack tier
		{280000, "¥2,800"}, // managed_ops base
		{1000000, "¥10,000"},
		{1000000000, "¥10,000,000"},
	}
	for _, c := range cases {
		got := FormatPriceCNY(c.cents)
		if got != c.want {
			t.Errorf("FormatPriceCNY(%d) = %q; want %q", c.cents, got, c.want)
		}
	}
}

func TestCategoryLabel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"diy_agent", "DIY 智能体"},
		{"content_pack", "资料制作包"},
		{"managed_ops", "代运营"},
		{"unknown", "unknown"},
	}
	for _, c := range cases {
		if got := CategoryLabel(c.in); got != c.want {
			t.Errorf("CategoryLabel(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
