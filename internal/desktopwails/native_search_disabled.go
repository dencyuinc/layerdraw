// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build !ladybug_native

package desktopwails

import (
	"context"

	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/host"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
)

func packagedNativeSearchEnabled() bool { return false }

func packagedNativeSearchLifecycle(*sharedOwner) desktopapp.NativeSearchLifecycle { return nil }

func openPackagedNativeSearch(string, *localdocument.Host, *endpoint.HostEngineFacade) (host.ConsumerSearchSurface, host.SearchDocumentLifecycle, func(), error) {
	return nil, nil, func() {}, nil
}

func invokePackagedNativeSearch(context.Context, *host.Endpoint, desktopcontract.Exchange) (desktopcontract.ExchangeResult, error, bool) {
	return desktopcontract.ExchangeResult{}, nil, false
}

func packagedNativeSearchDecoder() (any, bool) { return nil, false }
