package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sspgpt/v07/internal/model"
)

const shellSemanticsFilename = "sspgpt_semantics.json"

func loadShellSemantics(shellPath string) (*model.ShellSemantics, error) {
	shellPath = strings.TrimSpace(shellPath)
	if shellPath == "" {
		return nil, os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(filepath.Clean(shellPath), shellSemanticsFilename))
	if err != nil {
		return nil, err
	}
	var sem model.ShellSemantics
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sem); err != nil {
		return nil, err
	}
	if err := validateShellSemantics(&sem); err != nil {
		return nil, err
	}
	return &sem, nil
}

func validateShellSemantics(sem *model.ShellSemantics) error {
	if sem == nil || sem.FormatVersion != 1 {
		return errors.New("unsupported shell semantics format_version")
	}
	sem.ShellName = strings.TrimSpace(sem.ShellName)
	sem.DefaultPose = strings.TrimSpace(sem.DefaultPose)
	if sem.DefaultPose == "" {
		return errors.New("default_pose required")
	}
	poses := map[string]bool{}
	for i := range sem.Poses {
		sem.Poses[i].ID = strings.TrimSpace(sem.Poses[i].ID)
		sem.Poses[i].Meaning = strings.TrimSpace(sem.Poses[i].Meaning)
		if sem.Poses[i].ID == "" || sem.Poses[i].Meaning == "" {
			return errors.New("pose id and meaning required")
		}
		if poses[sem.Poses[i].ID] {
			return errors.New("duplicate pose id: " + sem.Poses[i].ID)
		}
		poses[sem.Poses[i].ID] = true
		for j := range sem.Poses[i].Uses {
			sem.Poses[i].Uses[j] = strings.TrimSpace(sem.Poses[i].Uses[j])
		}
	}
	if !poses[sem.DefaultPose] {
		return errors.New("default_pose not declared in poses")
	}
	normList := func(in []string, name string) ([]string, map[string]bool, error) {
		seen := map[string]bool{}
		out := make([]string, 0, len(in))
		for _, x := range in {
			x = strings.TrimSpace(x)
			if x == "" {
				continue
			}
			if seen[x] {
				return nil, nil, errors.New("duplicate " + name + ": " + x)
			}
			seen[x] = true
			out = append(out, x)
		}
		return out, seen, nil
	}
	var err error
	var expressions, gazes map[string]bool
	sem.Expressions, expressions, err = normList(sem.Expressions, "expression")
	if err != nil {
		return err
	}
	sem.Gazes, gazes, err = normList(sem.Gazes, "gaze")
	if err != nil {
		return err
	}
	if len(expressions) == 0 || len(gazes) == 0 {
		return errors.New("expressions and gazes required")
	}
	keys := map[string]bool{}
	for i := range sem.Surfaces {
		x := &sem.Surfaces[i]
		x.Pose = strings.TrimSpace(x.Pose)
		x.Expression = strings.TrimSpace(x.Expression)
		x.Gaze = strings.TrimSpace(x.Gaze)
		if !poses[x.Pose] || !expressions[x.Expression] || !gazes[x.Gaze] || x.Surface < 0 {
			return errors.New("surface combination references undeclared semantic id")
		}
		key := semanticSurfaceKey(x.Pose, x.Expression, x.Gaze)
		if keys[key] {
			return errors.New("duplicate surface combination: " + key)
		}
		keys[key] = true
	}
	if len(sem.Surfaces) == 0 {
		return errors.New("at least one surface combination required")
	}
	return nil
}

func semanticSurfaceKey(pose, expression, gaze string) string {
	return strings.TrimSpace(pose) + "\x00" + strings.TrimSpace(expression) + "\x00" + strings.TrimSpace(gaze)
}

func resolveSemanticSurface(rr model.Reaction, sem *model.ShellSemantics) (int, bool) {
	if sem == nil {
		return 0, false
	}
	pose := strings.TrimSpace(rr.Presentation.Pose)
	if pose == "" {
		return 0, false
	}
	expression := strings.TrimSpace(rr.Presentation.Expression)
	if expression == "" {
		expression = strings.TrimSpace(rr.ReactionEmotion)
	}
	gaze := strings.TrimSpace(rr.Presentation.Gaze)
	if gaze == "" {
		gaze = "normal"
	}
	find := func(p, e, g string) (int, bool) {
		for _, x := range sem.Surfaces {
			if x.Pose == p && x.Expression == e && x.Gaze == g {
				return x.Surface, true
			}
		}
		return 0, false
	}
	if n, ok := find(pose, expression, gaze); ok {
		return n, true
	}
	// A shell does not necessarily provide every Cartesian combination. Keep
	// the chosen pose authoritative, then degrade gaze and expression locally.
	if gaze != "normal" {
		if n, ok := find(pose, expression, "normal"); ok {
			return n, true
		}
	}
	if n, ok := find(pose, "neutral", gaze); ok {
		return n, true
	}
	if n, ok := find(pose, "neutral", "normal"); ok {
		return n, true
	}
	return 0, false
}

func embodimentForShell(shellPath string) (*model.EmbodimentCapabilities, *model.ShellSemantics, error) {
	sem, err := loadShellSemantics(shellPath)
	if err != nil {
		return nil, nil, err
	}
	cap := sem.Capabilities()
	if cap == nil {
		return nil, nil, errors.New("empty shell semantics capabilities")
	}
	return cap, sem, nil
}

func sortedPoseIDs(c *model.EmbodimentCapabilities) []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.Poses))
	for _, p := range c.Poses {
		if strings.TrimSpace(p.ID) != "" {
			out = append(out, p.ID)
		}
	}
	sort.Strings(out)
	return out
}
