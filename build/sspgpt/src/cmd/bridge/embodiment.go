package main

import (
	"fmt"
	"strings"

	"sspgpt/v07/internal/model"
)

func embodimentGuidance(c *model.EmbodimentCapabilities) string {
	if c == nil || len(c.Poses) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("The current Shell exposes the following body/presentation semantics. Choose only exact IDs listed here. Surface numbers are renderer details and must never be guessed or mentioned.\n")
	b.WriteString("presentation.pose is the sustained body pose/composition. presentation.gesture is only a transient action intent that is not represented by pose; it is NOT an alias or fallback for pose. Leave gesture empty when no separate transient action is needed.\n")
	b.WriteString("Available poses:\n")
	for _, p := range c.Poses {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			continue
		}
		b.WriteString("- " + id + ": " + strings.TrimSpace(p.Meaning))
		if len(p.Uses) > 0 {
			uses := make([]string, 0, len(p.Uses))
			for _, u := range p.Uses {
				if u = strings.TrimSpace(u); u != "" {
					uses = append(uses, u)
				}
			}
			if len(uses) > 0 {
				b.WriteString("; useful for " + strings.Join(uses, ", "))
			}
		}
		if len(p.SupportedExpressions) > 0 {
			b.WriteString("; expressions=" + strings.Join(p.SupportedExpressions, ","))
		}
		if len(p.SupportedGazes) > 0 {
			b.WriteString("; gazes=" + strings.Join(p.SupportedGazes, ","))
		}
		b.WriteByte('\n')
	}
	if len(c.Expressions) > 0 {
		b.WriteString("Available expressions: " + strings.Join(c.Expressions, ", ") + "\n")
	}
	if len(c.Gazes) > 0 {
		b.WriteString("Available gazes: " + strings.Join(c.Gazes, ", ") + "\n")
	}
	b.WriteString(fmt.Sprintf("Default pose when no special body composition is intended: %s", c.DefaultPose))
	return b.String()
}

func presentationSchema(env model.RequestEnvelope) map[string]any {
	pose := map[string]any{"type": "string", "enum": []string{"normal"}}
	expression := map[string]any{"type": "string"}
	gaze := map[string]any{"type": "string"}
	if c := env.Embodiment; c != nil {
		poses := make([]string, 0, len(c.Poses))
		for _, p := range c.Poses {
			if id := strings.TrimSpace(p.ID); id != "" {
				poses = append(poses, id)
			}
		}
		if len(poses) > 0 {
			pose["enum"] = poses
		}
		if len(c.Expressions) > 0 {
			expression["enum"] = c.Expressions
		}
		if len(c.Gazes) > 0 {
			gaze["enum"] = c.Gazes
		}
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"expression": expression,
			"pose":       pose,
			"gaze":       gaze,
			"gesture":    map[string]any{"type": "string"},
		},
		"required":             []string{"expression", "pose", "gaze", "gesture"},
		"additionalProperties": false,
	}
}
