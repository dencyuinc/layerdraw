// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"fmt"
	"strconv"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

type limitValues struct {
	maxProjectSourceFiles int64
	maxProjectSourceBytes int64
	maxPackFiles          int64
	maxPackBytes          int64
	maxAssets             int64
	maxAssetBytes         int64
	maxRasterDimension    int64
	maxRasterPixels       int64
	maxDeclarations       int64
}

func limitsToValues(limits engine.ResourceLimits) limitValues {
	return limitValues{
		maxProjectSourceFiles: limits.MaxProjectSourceFiles,
		maxProjectSourceBytes: limits.MaxProjectSourceBytes,
		maxPackFiles:          limits.MaxPackFiles,
		maxPackBytes:          limits.MaxPackBytes,
		maxAssets:             limits.MaxAssets,
		maxAssetBytes:         limits.MaxAssetBytes,
		maxRasterDimension:    limits.MaxRasterDimension,
		maxRasterPixels:       limits.MaxRasterPixels,
		maxDeclarations:       limits.MaxDeclarations,
	}
}

func (values limitValues) resourceLimits() engine.ResourceLimits {
	return engine.ResourceLimits{
		MaxProjectSourceFiles: values.maxProjectSourceFiles,
		MaxProjectSourceBytes: values.maxProjectSourceBytes,
		MaxPackFiles:          values.maxPackFiles,
		MaxPackBytes:          values.maxPackBytes,
		MaxAssets:             values.maxAssets,
		MaxAssetBytes:         values.maxAssetBytes,
		MaxRasterDimension:    values.maxRasterDimension,
		MaxRasterPixels:       values.maxRasterPixels,
		MaxDeclarations:       values.maxDeclarations,
	}
}

func validateLimitPolicy(policy LimitPolicy) error {
	defaults := limitsToValues(policy.Defaults)
	hard := limitsToValues(policy.HardMaximums)
	checks := []struct {
		name             string
		defaultValue     int64
		hardMaximumValue int64
	}{
		{"max_project_source_files", defaults.maxProjectSourceFiles, hard.maxProjectSourceFiles},
		{"max_project_source_bytes", defaults.maxProjectSourceBytes, hard.maxProjectSourceBytes},
		{"max_pack_files", defaults.maxPackFiles, hard.maxPackFiles},
		{"max_pack_bytes", defaults.maxPackBytes, hard.maxPackBytes},
		{"max_assets", defaults.maxAssets, hard.maxAssets},
		{"max_asset_bytes", defaults.maxAssetBytes, hard.maxAssetBytes},
		{"max_raster_dimension", defaults.maxRasterDimension, hard.maxRasterDimension},
		{"max_raster_pixels", defaults.maxRasterPixels, hard.maxRasterPixels},
		{"max_declarations", defaults.maxDeclarations, hard.maxDeclarations},
	}
	for _, check := range checks {
		if check.defaultValue <= 0 || check.hardMaximumValue <= 0 {
			return fmt.Errorf("limit %s default and hard maximum must be positive", check.name)
		}
		if check.defaultValue > check.hardMaximumValue {
			return fmt.Errorf("limit %s default exceeds its hard maximum", check.name)
		}
	}
	return nil
}

func effectiveLimits(policy LimitPolicy, constraints *protocolcommon.CompileResourceLimitConstraints) (engine.ResourceLimits, engine.ResourceLimits, error) {
	defaults := limitsToValues(policy.Defaults)
	effective := limitsToValues(policy.HardMaximums)
	if constraints != nil {
		var err error
		if effective.maxProjectSourceFiles, err = constrain(effective.maxProjectSourceFiles, constraints.MaxProjectSourceFiles); err != nil {
			return engine.ResourceLimits{}, engine.ResourceLimits{}, fmt.Errorf("max_project_source_files: %w", err)
		}
		if effective.maxProjectSourceBytes, err = constrain(effective.maxProjectSourceBytes, constraints.MaxProjectSourceBytes); err != nil {
			return engine.ResourceLimits{}, engine.ResourceLimits{}, fmt.Errorf("max_project_source_bytes: %w", err)
		}
		if effective.maxPackFiles, err = constrain(effective.maxPackFiles, constraints.MaxPackFiles); err != nil {
			return engine.ResourceLimits{}, engine.ResourceLimits{}, fmt.Errorf("max_pack_files: %w", err)
		}
		if effective.maxPackBytes, err = constrain(effective.maxPackBytes, constraints.MaxPackBytes); err != nil {
			return engine.ResourceLimits{}, engine.ResourceLimits{}, fmt.Errorf("max_pack_bytes: %w", err)
		}
		if effective.maxAssets, err = constrain(effective.maxAssets, constraints.MaxAssets); err != nil {
			return engine.ResourceLimits{}, engine.ResourceLimits{}, fmt.Errorf("max_assets: %w", err)
		}
		if effective.maxAssetBytes, err = constrain(effective.maxAssetBytes, constraints.MaxAssetBytes); err != nil {
			return engine.ResourceLimits{}, engine.ResourceLimits{}, fmt.Errorf("max_asset_bytes: %w", err)
		}
		if effective.maxRasterDimension, err = constrain(effective.maxRasterDimension, constraints.MaxRasterDimension); err != nil {
			return engine.ResourceLimits{}, engine.ResourceLimits{}, fmt.Errorf("max_raster_dimension: %w", err)
		}
		if effective.maxRasterPixels, err = constrain(effective.maxRasterPixels, constraints.MaxRasterPixels); err != nil {
			return engine.ResourceLimits{}, engine.ResourceLimits{}, fmt.Errorf("max_raster_pixels: %w", err)
		}
		if effective.maxDeclarations, err = constrain(effective.maxDeclarations, constraints.MaxDeclarations); err != nil {
			return engine.ResourceLimits{}, engine.ResourceLimits{}, fmt.Errorf("max_declarations: %w", err)
		}
	}

	cappedDefaults := limitValues{
		maxProjectSourceFiles: min(defaults.maxProjectSourceFiles, effective.maxProjectSourceFiles),
		maxProjectSourceBytes: min(defaults.maxProjectSourceBytes, effective.maxProjectSourceBytes),
		maxPackFiles:          min(defaults.maxPackFiles, effective.maxPackFiles),
		maxPackBytes:          min(defaults.maxPackBytes, effective.maxPackBytes),
		maxAssets:             min(defaults.maxAssets, effective.maxAssets),
		maxAssetBytes:         min(defaults.maxAssetBytes, effective.maxAssetBytes),
		maxRasterDimension:    min(defaults.maxRasterDimension, effective.maxRasterDimension),
		maxRasterPixels:       min(defaults.maxRasterPixels, effective.maxRasterPixels),
		maxDeclarations:       min(defaults.maxDeclarations, effective.maxDeclarations),
	}
	return cappedDefaults.resourceLimits(), effective.resourceLimits(), nil
}

func constrain(hard int64, constraint *protocolcommon.CanonicalPositiveInt64) (int64, error) {
	if constraint == nil {
		return hard, nil
	}
	value, err := strconv.ParseInt(string(*constraint), 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid positive client ceiling")
	}
	return min(hard, value), nil
}

func manifestLimits(policy LimitPolicy, effective engine.ResourceLimits) protocolcommon.CompileResourceLimitCapabilities {
	defaults := limitsToValues(policy.Defaults)
	hard := limitsToValues(policy.HardMaximums)
	maximums := limitsToValues(effective)
	return protocolcommon.CompileResourceLimitCapabilities{
		MaxAssetBytes:         byteLimit(defaults.maxAssetBytes, maximums.maxAssetBytes, hard.maxAssetBytes),
		MaxAssets:             itemLimit(defaults.maxAssets, maximums.maxAssets, hard.maxAssets),
		MaxDeclarations:       itemLimit(defaults.maxDeclarations, maximums.maxDeclarations, hard.maxDeclarations),
		MaxPackBytes:          byteLimit(defaults.maxPackBytes, maximums.maxPackBytes, hard.maxPackBytes),
		MaxPackFiles:          itemLimit(defaults.maxPackFiles, maximums.maxPackFiles, hard.maxPackFiles),
		MaxProjectSourceBytes: byteLimit(defaults.maxProjectSourceBytes, maximums.maxProjectSourceBytes, hard.maxProjectSourceBytes),
		MaxProjectSourceFiles: itemLimit(defaults.maxProjectSourceFiles, maximums.maxProjectSourceFiles, hard.maxProjectSourceFiles),
		MaxRasterDimension:    rasterDimensionLimit(defaults.maxRasterDimension, maximums.maxRasterDimension, hard.maxRasterDimension),
		MaxRasterPixels:       rasterPixelLimit(defaults.maxRasterPixels, maximums.maxRasterPixels, hard.maxRasterPixels),
	}
}

func operationLimits(limits engine.ResourceLimits) protocolcommon.CompileResourceLimitConstraints {
	values := limitsToValues(limits)
	return protocolcommon.CompileResourceLimitConstraints{
		MaxAssetBytes:         positive(values.maxAssetBytes),
		MaxAssets:             positive(values.maxAssets),
		MaxDeclarations:       positive(values.maxDeclarations),
		MaxPackBytes:          positive(values.maxPackBytes),
		MaxPackFiles:          positive(values.maxPackFiles),
		MaxProjectSourceBytes: positive(values.maxProjectSourceBytes),
		MaxProjectSourceFiles: positive(values.maxProjectSourceFiles),
		MaxRasterDimension:    positive(values.maxRasterDimension),
		MaxRasterPixels:       positive(values.maxRasterPixels),
	}
}

func positive(value int64) *protocolcommon.CanonicalPositiveInt64 {
	result := protocolcommon.CanonicalPositiveInt64(strconv.FormatInt(value, 10))
	return &result
}

func byteLimit(defaultValue, effectiveMaximum, hardMaximum int64) protocolcommon.ByteResourceLimitCapability {
	return protocolcommon.ByteResourceLimitCapability{
		DefaultValue:     *positive(defaultValue),
		EffectiveMaximum: *positive(effectiveMaximum),
		HardMaximum:      *positive(hardMaximum),
		Unit:             protocolcommon.ByteResourceLimitCapabilityUnitValue,
	}
}

func itemLimit(defaultValue, effectiveMaximum, hardMaximum int64) protocolcommon.ItemResourceLimitCapability {
	return protocolcommon.ItemResourceLimitCapability{
		DefaultValue:     *positive(defaultValue),
		EffectiveMaximum: *positive(effectiveMaximum),
		HardMaximum:      *positive(hardMaximum),
		Unit:             protocolcommon.ItemResourceLimitCapabilityUnitValue,
	}
}

func rasterDimensionLimit(defaultValue, effectiveMaximum, hardMaximum int64) protocolcommon.RasterDimensionResourceLimitCapability {
	return protocolcommon.RasterDimensionResourceLimitCapability{
		DefaultValue:     *positive(defaultValue),
		EffectiveMaximum: *positive(effectiveMaximum),
		HardMaximum:      *positive(hardMaximum),
		Unit:             protocolcommon.RasterDimensionResourceLimitCapabilityUnitValue,
	}
}

func rasterPixelLimit(defaultValue, effectiveMaximum, hardMaximum int64) protocolcommon.RasterPixelResourceLimitCapability {
	return protocolcommon.RasterPixelResourceLimitCapability{
		DefaultValue:     *positive(defaultValue),
		EffectiveMaximum: *positive(effectiveMaximum),
		HardMaximum:      *positive(hardMaximum),
		Unit:             protocolcommon.RasterPixelResourceLimitCapabilityUnitValue,
	}
}
