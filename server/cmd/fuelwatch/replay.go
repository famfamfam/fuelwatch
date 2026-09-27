package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"fuelwatch/internal/settings"
	"fuelwatch/internal/store"
	"fuelwatch/internal/visits"
	"fuelwatch/internal/worker"
)

// replay — прогон сохранённых кадров через модель и промпт без записи в БД (docs/05-backend.md §4.6):
//
//	fuelwatch replay --device cam-01 --from 2026-10-01 --to 2026-10-03 --model <id> --prompt v1 --out replay.csv
//
// Пишет CSV по кадрам и <out>.visits.csv — визиты, как их увидела бы логика.
func runReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	device := fs.String("device", "", "id устройства (обязательно)")
	fromS := fs.String("from", "", "начало: YYYY-MM-DD или RFC 3339 (по умолчанию — сутки назад)")
	toS := fs.String("to", "", "конец: YYYY-MM-DD или RFC 3339 (по умолчанию — сейчас)")
	model := fs.String("model", "", "модель (по умолчанию vision.model)")
	prompt := fs.String("prompt", "", "версия промпта (по умолчанию vision.prompt_version)")
	out := fs.String("out", "replay.csv", "CSV с ответами по кадрам")
	limit := fs.Int("limit", 2000, "не больше кадров")
	fs.Parse(args)
	if *device == "" {
		return fmt.Errorf("--device обязателен")
	}
	to := time.Now()
	from := to.Add(-24 * time.Hour)
	var err error
	if *fromS != "" {
		if from, err = parseDay(*fromS); err != nil {
			return err
		}
	}
	if *toS != "" {
		if to, err = parseDay(*toS); err != nil {
			return err
		}
		if len(*toS) == len("2006-01-02") {
			to = to.Add(24 * time.Hour)
		}
	}

	ctx := context.Background()
	pool, err := store.Open(ctx, os.Getenv("DATABASE_URL"), 10*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()
	if m := os.Getenv("VLM_MODEL"); m != "" {
		settings.SetDefault("vision.model", m)
	}
	s := &settings.Service{DB: pool}
	cls := newClassifier(s, os.Getenv("PUBLIC_URL"))
	vals, _, err := s.ForDevice(ctx, *device)
	if err != nil {
		return fmt.Errorf("device %s: %w", *device, err)
	}
	params := visits.ParamsFrom(vals)

	rows, err := pool.Query(ctx, `SELECT id, kind, taken_at FROM frames WHERE device_id=$1 AND kind IN ('change','keyframe')
		AND taken_at >= $2 AND taken_at < $3 ORDER BY taken_at LIMIT $4`, *device, from, to, *limit)
	if err != nil {
		return err
	}
	type fr struct {
		id, kind string
		at       time.Time
	}
	var frames []fr
	for rows.Next() {
		var f fr
		if err := rows.Scan(&f.id, &f.kind, &f.at); err != nil {
			return err
		}
		frames = append(frames, f)
	}
	rows.Close()
	fmt.Printf("replay %s: %d кадров %s — %s\n", *device, len(frames), from.Format(time.DateTime), to.Format(time.DateTime))

	file, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer file.Close()
	w := csv.NewWriter(file)
	w.Write([]string{"frame_id", "taken_at", "kind", "tanker_present", "tanker_in_zone", "confidence", "view_ok", "obstructed",
		"note", "model", "prompt", "cost", "latency_ms", "error", "event"})

	var st visits.State
	type visit struct{ from, to time.Time }
	var found []visit
	var cost float64
	var errorsN int
	for i, f := range frames {
		in, err := worker.LoadInput(ctx, pool, env("FRAMES_DIR", "./data/frames"), f.id)
		if err != nil {
			return err
		}
		in.Model, in.PromptVersion = *model, *prompt
		obs, meta, cerr := cls.Classify(ctx, in)
		cost += meta.Cost
		event := ""
		errText := ""
		if cerr != nil {
			errText = cerr.Error()
			errorsN++
		} else {
			prev := st.Open
			d := visits.Step(st, visits.Obs{TankerPresent: obs.TankerPresent, TankerInZone: obs.TankerInZone,
				Obstructed: obs.ViewObstructed, Confidence: obs.Confidence, FrameID: f.id}, f.at, params)
			st, event = d.State, string(d.Event)
			if d.Event == visits.Left {
				found = append(found, visit{prev.StartedAt, d.EndedAt})
			}
		}
		w.Write([]string{f.id, f.at.Local().Format(time.DateTime), f.kind, strconv.FormatBool(obs.TankerPresent),
			strconv.FormatBool(obs.TankerInZone), fmt.Sprintf("%.2f", obs.Confidence), strconv.FormatBool(obs.ViewMatchesReference),
			strconv.FormatBool(obs.ViewObstructed), obs.Note, meta.Model, meta.PromptVersion, fmt.Sprintf("%.6f", meta.Cost),
			strconv.FormatInt(meta.Latency.Milliseconds(), 10), errText, event})
		if (i+1)%20 == 0 {
			w.Flush()
			fmt.Printf("  %d/%d, $%.4f\n", i+1, len(frames), cost)
		}
	}
	if st.Open != nil {
		found = append(found, visit{st.Open.StartedAt, time.Time{}})
	}
	w.Flush()

	vf, err := os.Create(strings.TrimSuffix(*out, ".csv") + ".visits.csv")
	if err != nil {
		return err
	}
	defer vf.Close()
	vw := csv.NewWriter(vf)
	vw.Write([]string{"started_at", "ended_at", "minutes"})
	for _, v := range found {
		end, mins := "идёт", ""
		if !v.to.IsZero() {
			end, mins = v.to.Local().Format(time.DateTime), strconv.Itoa(int(v.to.Sub(v.from).Minutes()))
		}
		vw.Write([]string{v.from.Local().Format(time.DateTime), end, mins})
		fmt.Printf("  визит %s — %s\n", v.from.Local().Format(time.DateTime), end)
	}
	vw.Flush()
	fmt.Printf("готово: визитов %d, ошибок %d, стоимость $%.4f → %s\n", len(found), errorsN, cost, *out)
	return nil
}

func parseDay(s string) (time.Time, error) {
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
