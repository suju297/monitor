package monitor

import "testing"

func TestMyGreenhouseDefaultCommandUsesModuleExecution(t *testing.T) {
	t.Parallel()

	want := []string{"uv", "run", "python", "-m", "scripts.fetch_my_greenhouse_jobs"}
	if len(MyGreenhouseDefaultCommand) != len(want) {
		t.Fatalf("default command len = %d, want %d", len(MyGreenhouseDefaultCommand), len(want))
	}
	for i, part := range want {
		if MyGreenhouseDefaultCommand[i] != part {
			t.Fatalf("default command[%d] = %q, want %q", i, MyGreenhouseDefaultCommand[i], part)
		}
	}
}
