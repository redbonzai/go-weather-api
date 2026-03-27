package nws

import "testing"

func TestClassifyTemperatureF(t *testing.T) {
	tests := []struct {
		f    float64
		want string
	}{
		{32, "cold"},
		{49, "cold"},
		{50, "moderate"},
		{70, "moderate"},
		{82, "moderate"},
		{83, "hot"},
		{100, "hot"},
	}
	for _, tt := range tests {
		if got := ClassifyTemperatureF(tt.f); got != tt.want {
			t.Errorf("ClassifyTemperatureF(%v) = %q, want %q", tt.f, got, tt.want)
		}
	}
}

func TestTempFahrenheit(t *testing.T) {
	if got := tempFahrenheit(0, "C"); got < 31.9 || got > 32.1 {
		t.Fatalf("0C -> F: got %v", got)
	}
	if got := tempFahrenheit(72, "F"); got != 72 {
		t.Fatalf("72F: got %v", got)
	}
}
