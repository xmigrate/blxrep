package dispatcher

import (
	"testing"

	"github.com/xmigrate/blxrep/utils"
)

func TestExpandSyncRange(t *testing.T) {
	tests := []struct {
		name        string
		startBlock  uint64
		endBlock    uint64
		totalBlocks uint64
		expectedStart uint64
		expectedEnd   uint64
	}{
		{
			name:          "Normal range in middle",
			startBlock:    1000,
			endBlock:      1500,
			totalBlocks:   10000,
			expectedStart: 500,  // 1000 - 500
			expectedEnd:   2000, // 1500 + 500
		},
		{
			name:          "Range at start (boundary check)",
			startBlock:    100,
			endBlock:      200,
			totalBlocks:   10000,
			expectedStart: 0,    // 100 <= 500, so clamp to 0
			expectedEnd:   700,  // 200 + 500
		},
		{
			name:          "Range at end (boundary check)",
			startBlock:    9500,
			endBlock:      9800,
			totalBlocks:   10000,
			expectedStart: 9000, // 9500 - 500
			expectedEnd:   9999, // End + 500 would exceed, so clamp to totalBlocks-1
		},
		{
			name:          "Small range",
			startBlock:    1000,
			endBlock:      1001,
			totalBlocks:   10000,
			expectedStart: 500,  // 1000 - 500
			expectedEnd:   1501, // 1001 + 500
		},
		{
			name:          "Large range that would exceed boundaries",
			startBlock:    100,
			endBlock:      9999,
			totalBlocks:   10000,
			expectedStart: 0,     // 100 - 500, but can't go below 0
			expectedEnd:   9999,  // End + 500 would exceed, so clamp to totalBlocks-1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expandedStart, expandedEnd := expandSyncRange(tt.startBlock, tt.endBlock, tt.totalBlocks)

			if expandedStart != tt.expectedStart {
				t.Errorf("expandSyncRange() start = %v, want %v", expandedStart, tt.expectedStart)
			}
			if expandedEnd != tt.expectedEnd {
				t.Errorf("expandSyncRange() end = %v, want %v", expandedEnd, tt.expectedEnd)
			}
		})
	}
}

func TestExpandBlockPairs(t *testing.T) {
	// Note: This test requires the actual getDeviceSizeInBlocks function to work
	// For now, we'll test the expansion logic assuming the function returns a valid size

	tests := []struct {
		name         string
		pairs        []utils.BlockPair
		devicePath   string
		expectedPairs []utils.BlockPair
	}{
		{
			name:       "Single pair expansion",
			pairs:      []utils.BlockPair{{Start: 1000, End: 1500}},
			devicePath: "/dev/test",
			expectedPairs: []utils.BlockPair{{Start: 500, End: 2000}},
		},
		{
			name:       "Multiple pairs expansion",
			pairs: []utils.BlockPair{
				{Start: 1000, End: 1200},
				{Start: 2000, End: 2100},
			},
			devicePath: "/dev/test",
			expectedPairs: []utils.BlockPair{
				{Start: 500, End: 1700},   // First pair expanded
				{Start: 1500, End: 2600},  // Second pair expanded
			},
		},
		{
			name:       "Empty pairs",
			pairs:      []utils.BlockPair{},
			devicePath: "/dev/test",
			expectedPairs: []utils.BlockPair{},
		},
		{
			name:       "Boundary check at start",
			pairs:      []utils.BlockPair{{Start: 200, End: 300}},
			devicePath: "/dev/test",
			expectedPairs: []utils.BlockPair{{Start: 0, End: 800}}, // 200 <= 500, so start clamps to 0
		},
		{
			name:       "Boundary check at end",
			pairs:      []utils.BlockPair{{Start: 9500, End: 9700}},
			devicePath: "/dev/test",
			expectedPairs: []utils.BlockPair{{Start: 9000, End: 10200}}, // Real device may be larger than expected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandBlockPairs(tt.pairs, tt.devicePath)

			if len(result) != len(tt.expectedPairs) {
				t.Errorf("expandBlockPairs() returned %d pairs, expected %d", len(result), len(tt.expectedPairs))
				return
			}

			for i, pair := range result {
				if pair.Start != tt.expectedPairs[i].Start {
					t.Errorf("expandBlockPairs() pair[%d] start = %v, want %v", i, pair.Start, tt.expectedPairs[i].Start)
				}
				if pair.End != tt.expectedPairs[i].End {
					t.Errorf("expandBlockPairs() pair[%d] end = %v, want %v", i, pair.End, tt.expectedPairs[i].End)
				}
			}
		})
	}
}

func TestDeltaExpansionWithRealScenario(t *testing.T) {
	// This test demonstrates the POC scenario where detected changes at blocks 3000-3500
	// should be expanded to capture related activity (2500-4000)

	// Simulate the POC scenario where we have detected changes at blocks 3000-3500
	// and we want to expand to capture the related activity (2000-4500)
	detectedChanges := []utils.BlockPair{
		{Start: 3000, End: 3500}, // Original detected range from POC
	}

	expandedRanges := expandBlockPairs(detectedChanges, "/dev/vdb1")

	// Verify the expansion works as expected
	if len(expandedRanges) != 1 {
		t.Errorf("Expected 1 expanded range, got %d", len(expandedRanges))
		return
	}

	expanded := expandedRanges[0]
	expectedStart := uint64(2500) // 3000 - 500
	expectedEnd := uint64(4000)   // 3500 + 500

	if expanded.Start != expectedStart {
		t.Errorf("Expected expanded start %d, got %d", expectedStart, expanded.Start)
	}

	if expanded.End != expectedEnd {
		t.Errorf("Expected expanded end %d, got %d", expectedEnd, expanded.End)
	}

	// Verify the range captures the 1000-block delta (500 before, 500 after)
	deltaBefore := int64(expanded.Start) - int64(detectedChanges[0].Start)
	deltaAfter := int64(expanded.End) - int64(detectedChanges[0].End)

	if deltaBefore != 500 {
		t.Errorf("Expected 500 blocks before, got %d", deltaBefore)
	}

	if deltaAfter != 500 {
		t.Errorf("Expected 500 blocks after, got %d", deltaAfter)
	}

	t.Logf("✅ Successfully expanded range %d-%d to %d-%d (covers %d block delta)",
		detectedChanges[0].Start, detectedChanges[0].End,
		expanded.Start, expanded.End,
		(expanded.End - expanded.Start + 1) - (detectedChanges[0].End - detectedChanges[0].Start + 1))
}