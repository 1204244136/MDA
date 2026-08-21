package equipmentreroll

import "testing"

func TestIsFourAtkTemplateFromNodeJSON(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "four attack four elemental",
			raw:  `{"recognition":{"param":{"custom_recognition_param":{"template":"FourAttackFourElementalDamage"}}}}`,
			want: true,
		},
		{
			name: "four attack alias",
			raw:  `{"recognition":{"param":{"custom_recognition_param":{"template":"FourAtkFourElem"}}}}`,
			want: true,
		},
		{
			name: "four elemental",
			raw:  `{"recognition":{"param":{"custom_recognition_param":{"template":"FourElementalDamage"}}}}`,
			want: false,
		},
		{
			name: "empty",
			raw:  "",
			want: false,
		},
		{
			name: "invalid json",
			raw:  `not-json`,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFourAtkTemplateFromNodeJSON(tc.raw); got != tc.want {
				t.Fatalf("isFourAtkTemplateFromNodeJSON(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
