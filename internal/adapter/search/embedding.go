// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package search

import (
	"context"
	"errors"
	"math"
	"unicode/utf8"

	"github.com/dencyuinc/layerdraw/internal/runtime/port"
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
}

func NewEmbeddingProvider(capability port.EmbeddingCapability, models map[string]VectorModel, allowRemote bool) (*ConfiguredEmbeddingProvider, error) {
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
	for _, profile := range capability.Profiles {
		if profile.ProfileID == "" || profile.ModelDigest == "" || profile.Dimensions <= 0 || profile.MaxInputBytes <= 0 || cloned[profile.ProfileID] == nil {
			return nil, ErrEmbeddingProfileMismatch
		}
	}
	capability.Profiles = append([]port.EmbeddingProfile(nil), capability.Profiles...)
	return &ConfiguredEmbeddingProvider{capability: capability, models: cloned}, nil
}

func (p *ConfiguredEmbeddingProvider) Describe(context.Context) (port.EmbeddingCapability, error) {
	result := p.capability
	result.Profiles = append([]port.EmbeddingProfile(nil), result.Profiles...)
	return result, nil
}

func (p *ConfiguredEmbeddingProvider) EmbedDocuments(ctx context.Context, profile port.EmbeddingProfile, documents []port.SearchDocumentInput) ([]port.EmbeddingVector, error) {
	model, err := p.model(profile)
	if err != nil {
		return nil, err
	}
	result := make([]port.EmbeddingVector, 0, len(documents))
	for _, document := range documents {
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
