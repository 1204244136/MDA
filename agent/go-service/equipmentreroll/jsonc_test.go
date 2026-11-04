package equipmentreroll

import (
	"encoding/json"

	"github.com/1204244136/MDA/agent/go-service/pkg/jsonclean"
)

func unmarshalJSONC(raw []byte, out any) error {
	return json.Unmarshal(jsonclean.Clean(raw), out)
}
