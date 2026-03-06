package vector

import (
	"math"
	"testing"
)

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1, 2, 3}
	got := CosineSimilarity(a, a)
	if math.Abs(float64(got-1.0)) > 1e-6 {
		t.Errorf("identical vectors: got %f, want 1.0", got)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	got := CosineSimilarity(a, b)
	if math.Abs(float64(got)) > 1e-6 {
		t.Errorf("orthogonal vectors: got %f, want 0.0", got)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	got := CosineSimilarity(a, b)
	if math.Abs(float64(got+1.0)) > 1e-6 {
		t.Errorf("opposite vectors: got %f, want -1.0", got)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	got := CosineSimilarity(a, b)
	if got != 0 {
		t.Errorf("zero vector: got %f, want 0.0", got)
	}
}

func TestCosineSimilarity_MismatchedLengths(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{1, 2, 3}
	got := CosineSimilarity(a, b)
	if got != 0 {
		t.Errorf("mismatched lengths: got %f, want 0.0", got)
	}
}

func TestCosineSimilarity_Empty(t *testing.T) {
	got := CosineSimilarity(nil, nil)
	if got != 0 {
		t.Errorf("empty vectors: got %f, want 0.0", got)
	}
}

func TestSerializeDeserializeRoundTrip(t *testing.T) {
	original := []float32{1.5, -2.3, 0.0, 42.0, math.MaxFloat32}
	serialized := SerializeEmbedding(original)
	deserialized := DeserializeEmbedding(serialized)

	if len(deserialized) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(deserialized), len(original))
	}

	for i, v := range original {
		if deserialized[i] != v {
			t.Errorf("index %d: got %f, want %f", i, deserialized[i], v)
		}
	}
}

func TestDeserializeInvalidLength(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	result := DeserializeEmbedding(data)
	if result != nil {
		t.Errorf("non-4-aligned bytes should return nil, got %v", result)
	}
}

func TestDeserializeEmpty(t *testing.T) {
	result := DeserializeEmbedding(nil)
	if len(result) != 0 {
		t.Errorf("nil input should return empty, got len %d", len(result))
	}
}

func TestSerializeEmpty(t *testing.T) {
	result := SerializeEmbedding(nil)
	if len(result) != 0 {
		t.Errorf("nil input should return empty slice, got len %d", len(result))
	}
}
