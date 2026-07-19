// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package search

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/internal/runtime/port"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrEmbeddingUnavailable     = errors.New("embedding provider unavailable")
	ErrEmbeddingProfileMismatch = errors.New("embedding profile mismatch")
	ErrRemoteEmbeddingDenied    = errors.New("remote embedding requires explicit host policy")
)

type VectorModel interface {
	Embed(context.Context, string) ([]float32, error)
}

type ConfiguredEmbeddingProvider struct {
	capability port.EmbeddingCapability
	models     map[string]VectorModel
	verifier   port.SearchDocumentBatchVerifier
}

func NewEmbeddingProvider(capability port.EmbeddingCapability, models map[string]VectorModel, allowRemote bool, verifier port.SearchDocumentBatchVerifier) (*ConfiguredEmbeddingProvider, error) {
	if capability.ProviderID == "" || !capability.Available {
		return nil, ErrEmbeddingUnavailable
	}
	if capability.Remote && !allowRemote {
		return nil, ErrRemoteEmbeddingDenied
	}
	cloned := make(map[string]VectorModel, len(models))
	for key, model := range models {
		if key != "" && model != nil {
			cloned[key] = model
		}
	}
	if verifier == nil {
		return nil, ErrEmbeddingProfileMismatch
	}
	for _, profile := range capability.Profiles {
		if profile.ProfileID == "" || profile.ModelDigest == "" || profile.Dimensions <= 0 || profile.MaxInputBytes <= 0 || cloned[profile.ProfileID] == nil {
			return nil, ErrEmbeddingProfileMismatch
		}
	}
	capability.Profiles = append([]port.EmbeddingProfile(nil), capability.Profiles...)
	return &ConfiguredEmbeddingProvider{capability: capability, models: cloned, verifier: verifier}, nil
}

func (p *ConfiguredEmbeddingProvider) Describe(context.Context) (port.EmbeddingCapability, error) {
	result := p.capability
	result.Profiles = append([]port.EmbeddingProfile(nil), result.Profiles...)
	return result, nil
}

func (p *ConfiguredEmbeddingProvider) EmbedDocuments(ctx context.Context, profile port.EmbeddingProfile, batch port.SearchDocumentBatch) ([]port.EmbeddingVector, error) {
	if err := p.verifier.VerifySearchDocumentBatch(ctx, batch); err != nil || batch.EmbeddingProfileDigest != profile.ModelDigest {
		return nil, ErrEmbeddingProfileMismatch
	}
	model, err := p.model(profile)
	if err != nil {
		return nil, err
	}
	result := make([]port.EmbeddingVector, 0, len(batch.Documents))
	seen := map[string]bool{}
	for _, document := range batch.Documents {
		if seen[document.SubjectAddress] {
			return nil, ErrEmbeddingProfileMismatch
		}
		seen[document.SubjectAddress] = true
		if document.SubjectAddress == "" || document.ContentHash == "" || document.Text == "" || !utf8.ValidString(document.Text) || len([]byte(document.Text)) > profile.MaxInputBytes {
			return nil, ErrEmbeddingProfileMismatch
		}
		values, err := model.Embed(ctx, document.Text)
		if err != nil {
			return nil, err
		}
		if err := validateVector(values, profile.Dimensions); err != nil {
			return nil, err
		}
		result = append(result, port.EmbeddingVector{SubjectAddress: document.SubjectAddress, ContentHash: document.ContentHash, Values: append([]float32(nil), values...)})
	}
	return result, nil
}

// hmacSearchDocumentAuthority cryptographically binds the exact ordered batch
// to snapshot, Access projection and Embedding Profile. Its issuer is private;
// host consumers receive only a verifier.
type hmacSearchDocumentAuthority struct{ key []byte }

func newHMACSearchDocumentAuthority(key []byte) (*hmacSearchDocumentAuthority, error) {
	if len(key) < 32 {
		return nil, ErrEmbeddingProfileMismatch
	}
	return &hmacSearchDocumentAuthority{key: append([]byte(nil), key...)}, nil
}
func (a *hmacSearchDocumentAuthority) issue(batch port.SearchDocumentBatch) (port.SearchDocumentBatch, error) {
	batch.Token = ""
	data, err := json.Marshal(batch)
	if err != nil {
		return batch, err
	}
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write(data)
	batch.Token = base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return batch, nil
}
func (a *hmacSearchDocumentAuthority) VerifySearchDocumentBatch(_ context.Context, batch port.SearchDocumentBatch) error {
	token := batch.Token
	batch.Token = ""
	data, err := json.Marshal(batch)
	if err != nil {
		return ErrEmbeddingProfileMismatch
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return ErrEmbeddingProfileMismatch
	}
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write(data)
	if !hmac.Equal(decoded, mac.Sum(nil)) {
		return ErrEmbeddingProfileMismatch
	}
	return nil
}

func NewSearchDocumentBatchVerifier(key []byte) (port.SearchDocumentBatchVerifier, error) {
	return newHMACSearchDocumentAuthority(key)
}

// LocalProjectionModel is a concrete, deterministic, offline embedding model.
// Its version/digest are fixed by the EmbeddingProfile supplied at composition.
type LocalProjectionModel struct {
	dimensions int
	seed       []byte
}

func NewLocalProjectionModel(dimensions int, seed []byte) (*LocalProjectionModel, error) {
	if dimensions <= 0 || len(seed) < 16 {
		return nil, ErrEmbeddingProfileMismatch
	}
	return &LocalProjectionModel{dimensions: dimensions, seed: append([]byte(nil), seed...)}, nil
}
func (m *LocalProjectionModel) Embed(ctx context.Context, text string) ([]float32, error) {
	values := make([]float32, m.dimensions)
	tokens := strings.Fields(strings.ToLower(norm.NFC.String(text)))
	if len(tokens) == 0 {
		return nil, ErrEmbeddingProfileMismatch
	}
	for _, token := range tokens {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		mac := hmac.New(sha256.New, m.seed)
		_, _ = mac.Write([]byte(token))
		sum := mac.Sum(nil)
		for offset := 0; offset+8 <= len(sum); offset += 8 {
			v := binary.LittleEndian.Uint64(sum[offset : offset+8])
			index := int(v % uint64(m.dimensions))
			if v&(1<<63) == 0 {
				values[index]++
			} else {
				values[index]--
			}
		}
	}
	var norm2 float64
	for _, value := range values {
		norm2 += float64(value * value)
	}
	if norm2 == 0 {
		return nil, ErrEmbeddingProfileMismatch
	}
	scale := float32(1 / math.Sqrt(norm2))
	for i := range values {
		values[i] *= scale
	}
	return values, nil
}

func (p *ConfiguredEmbeddingProvider) EmbedQuery(ctx context.Context, profile port.EmbeddingProfile, text string) ([]float32, error) {
	model, err := p.model(profile)
	if err != nil {
		return nil, err
	}
	if text == "" || !utf8.ValidString(text) || len([]byte(text)) > profile.MaxInputBytes {
		return nil, ErrEmbeddingProfileMismatch
	}
	values, err := model.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	if err := validateVector(values, profile.Dimensions); err != nil {
		return nil, err
	}
	return append([]float32(nil), values...), nil
}

func (p *ConfiguredEmbeddingProvider) model(profile port.EmbeddingProfile) (VectorModel, error) {
	for _, configured := range p.capability.Profiles {
		if configured == profile {
			if model := p.models[profile.ProfileID]; model != nil {
				return model, nil
			}
		}
	}
	return nil, ErrEmbeddingProfileMismatch
}

func validateVector(values []float32, dimensions int) error {
	if len(values) != dimensions {
		return ErrEmbeddingProfileMismatch
	}
	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return ErrEmbeddingProfileMismatch
		}
	}
	return nil
}
