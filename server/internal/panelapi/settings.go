package panelapi

import (
	"net/http"

	"fuelwatch/internal/httpx"
	"fuelwatch/internal/settings"
	"fuelwatch/internal/store"
)

// GET /settings/schema?device= — реестр; с device диапазоны и варианты берутся из camera_caps.
func (a *API) settingsSchema(w http.ResponseWriter, r *http.Request) {
	var caps *settings.Caps
	if id := r.URL.Query().Get("device"); id != "" {
		d, err := store.GetDevice(r.Context(), a.DB, id)
		if err == store.ErrNotFound {
			httpx.Error(w, http.StatusNotFound, "not_found", "device not found")
			return
		}
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		caps = settings.ParseCaps(d.CameraCaps)
	}
	defs := make([]settings.Def, 0, len(settings.All()))
	for _, d := range settings.All() {
		defs = append(defs, d.ForCaps(caps))
	}
	httpx.OK(w, map[string]any{"groups": settings.Groups, "settings": defs})
}

func (a *API) getGlobalSettings(w http.ResponseWriter, r *http.Request) {
	g, err := a.Settings.Global(r.Context())
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.OK(w, g)
}

func (a *API) putGlobalSettings(w http.ResponseWriter, r *http.Request) {
	var changes map[string]any
	if !httpx.Decode(w, r, &changes, maxJSONBody) {
		return
	}
	errs, err := a.Settings.SetGlobal(r.Context(), changes, userFrom(r).ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if len(errs) > 0 {
		httpx.FieldErrors(w, errs)
		return
	}
	a.Hub.Publish("settings.updated", map[string]any{"scope": "global"})
	a.getGlobalSettings(w, r)
}

type settingValue struct {
	Value  any    `json:"value"`
	Source string `json:"source"`
}

func (a *API) getDeviceSettings(w http.ResponseWriter, r *http.Request) {
	d := a.loadDevice(w, r)
	if d == nil {
		return
	}
	vals, src, err := a.Settings.ForDevice(r.Context(), d.ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	out := make(map[string]settingValue, len(vals))
	for k, v := range vals {
		out[k] = settingValue{Value: v, Source: src[k]}
	}
	httpx.OK(w, out)
}

func (a *API) putDeviceSettings(w http.ResponseWriter, r *http.Request) {
	d := a.loadDevice(w, r)
	if d == nil {
		return
	}
	var changes map[string]any
	if !httpx.Decode(w, r, &changes, maxJSONBody) {
		return
	}
	errs, err := a.Settings.SetDevice(r.Context(), d.ID, changes)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if len(errs) > 0 {
		httpx.FieldErrors(w, errs)
		return
	}
	a.Hub.Publish("settings.updated", map[string]any{"scope": "device", "device_id": d.ID})
	a.publishStatus(r.Context(), d.ID)
	a.getDeviceSettings(w, r)
}
