package diff

import (
	"bytes"
	"testing"
)

func TestNewDiffEngine(t *testing.T) {
	tests := []struct {
		name    string
		library string
		wantErr bool
	}{
		{"bsdiff engine", "bsdiff", false},
		{"xdelta engine (not implemented)", "xdelta", true},
		{"invalid engine", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := NewDiffEngine(tt.library)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDiffEngine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && engine == nil {
				t.Error("NewDiffEngine() returned nil engine without error")
			}
		})
	}
}

func TestBsdiffEngine_ComputeDiffAndPatch(t *testing.T) {
	engine := NewBsdiffEngine()

	tests := []struct {
		name     string
		oldData  []byte
		newData  []byte
		wantErr  bool
		testName string
	}{
		{
			name:    "identical data",
			oldData: []byte("hello world"),
			newData: []byte("hello world"),
			wantErr: false,
		},
		{
			name:    "simple change",
			oldData: []byte("hello world"),
			newData: []byte("hello mars!"),
			wantErr: false,
		},
		{
			name:    "empty old data (new file)",
			oldData: []byte{},
			newData: []byte("new file content"),
			wantErr: false,
		},
		{
			name:    "empty new data (file deletion)",
			oldData: []byte("old file content"),
			newData: []byte{},
			wantErr: false,
		},
		{
			name:    "both empty",
			oldData: []byte{},
			newData: []byte{},
			wantErr: false,
		},
		{
			name:    "large change",
			oldData: bytes.Repeat([]byte("A"), 10000),
			newData: bytes.Repeat([]byte("B"), 10000),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compute diff
			patch, err := engine.ComputeDiff(tt.oldData, tt.newData)
			if (err != nil) != tt.wantErr {
				t.Errorf("ComputeDiff() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Apply patch to verify round-trip
			reconstructed, err := engine.ApplyPatch(tt.oldData, patch)
			if err != nil {
				t.Errorf("ApplyPatch() error = %v", err)
				return
			}

			// Verify reconstructed data matches new data
			if !bytes.Equal(reconstructed, tt.newData) {
				t.Errorf("Round-trip failed: reconstructed data doesn't match new data")
			}
		})
	}
}

func TestBsdiffEngine_Name(t *testing.T) {
	engine := NewBsdiffEngine()
	if engine.Name() != "bsdiff" {
		t.Errorf("Name() = %s, want 'bsdiff'", engine.Name())
	}
}

func TestComputeStats(t *testing.T) {
	oldData := []byte("hello world")
	newData := []byte("hello mars!")
	patchData := []byte("small patch")

	stats := ComputeStats(oldData, newData, patchData)

	if stats.OldSize != len(oldData) {
		t.Errorf("OldSize = %d, want %d", stats.OldSize, len(oldData))
	}

	if stats.NewSize != len(newData) {
		t.Errorf("NewSize = %d, want %d", stats.NewSize, len(newData))
	}

	if stats.PatchSize != len(patchData) {
		t.Errorf("PatchSize = %d, want %d", stats.PatchSize, len(patchData))
	}

	expectedRate := float64(len(patchData)) / float64(len(newData))
	if stats.CompressionRate != expectedRate {
		t.Errorf("CompressionRate = %f, want %f", stats.CompressionRate, expectedRate)
	}
}

func TestComputeStats_EmptyNewData(t *testing.T) {
	stats := ComputeStats([]byte("old"), []byte{}, []byte{})

	if stats.CompressionRate != 0 {
		t.Errorf("CompressionRate for empty new data = %f, want 0", stats.CompressionRate)
	}
}

// Benchmark tests
func BenchmarkBsdiffComputeDiff_SmallFile(b *testing.B) {
	engine := NewBsdiffEngine()
	oldData := bytes.Repeat([]byte("hello world "), 100) // ~1.2KB
	newData := bytes.Repeat([]byte("hello mars! "), 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.ComputeDiff(oldData, newData)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBsdiffComputeDiff_MediumFile(b *testing.B) {
	engine := NewBsdiffEngine()
	oldData := bytes.Repeat([]byte("A"), 1024*1024) // 1MB
	newData := append(oldData[:len(oldData)/2], bytes.Repeat([]byte("B"), 1024*1024/2)...)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.ComputeDiff(oldData, newData)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBsdiffApplyPatch_SmallFile(b *testing.B) {
	engine := NewBsdiffEngine()
	oldData := bytes.Repeat([]byte("hello world "), 100)
	newData := bytes.Repeat([]byte("hello mars! "), 100)

	patch, err := engine.ComputeDiff(oldData, newData)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.ApplyPatch(oldData, patch)
		if err != nil {
			b.Fatal(err)
		}
	}
}
