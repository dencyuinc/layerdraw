// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build !ladybug_native

package desktopwails

import (
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/host"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
)

func packagedNativeSearchEnabled() bool { return false }

func openPackagedNativeSearch(string, *localdocument.Host, *endpoint.HostEngineFacade) (host.ConsumerSearchSurface, host.SearchDocumentLifecycle, func(), error) {
	return nil, nil, func() {}, nil
}
