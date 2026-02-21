package notifications

import "testing"

func TestExtractYasnoGroupFromCalendarID(t *testing.T) {
	tests := []struct {
		name       string
		calendarID string
		want       string
	}{
		{
			name:       "Standard Yasno Kyiv calendar 2.1",
			calendarID: "calendar.yasno_kiiv_2_1_planned_outages",
			want:       "група 2.1",
		},
		{
			name:       "Standard Yasno Kyiv calendar 3.2",
			calendarID: "calendar.yasno_kyiv_3_2_planned_outages",
			want:       "група 3.2",
		},
		{
			name:       "Calendar with different spelling",
			calendarID: "calendar.yasno_kiev_1_1_planned_outages",
			want:       "група 1.1",
		},
		{
			name:       "Without calendar prefix",
			calendarID: "yasno_kiiv_2_1_planned_outages",
			want:       "група 2.1",
		},
		{
			name:       "Empty string",
			calendarID: "",
			want:       "",
		},
		{
			name:       "No group pattern",
			calendarID: "calendar.other_calendar",
			want:       "",
		},
		{
			name:       "Group at end",
			calendarID: "calendar.yasno_outages_4_2",
			want:       "група 4.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractYasnoGroupFromCalendarID(tt.calendarID)
			if got != tt.want {
				t.Errorf("ExtractYasnoGroupFromCalendarID(%q) = %q, want %q", tt.calendarID, got, tt.want)
			}
		})
	}
}
