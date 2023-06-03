package preview

import (
	"github.com/samber/lo"
)

type Feature = string

const (
	FeatureWebsocket Feature = "websocket"
)

var enabledFeatures []Feature = nil

func IsEnabled(feat Feature) bool {
	if enabledFeatures == nil {
		// Get enabled features from project file
	}

	return lo.Contains(enabledFeatures, feat)
}
