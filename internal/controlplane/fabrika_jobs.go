package controlplane

import (
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/owainlewis/machinist/internal/tickets"
)

func (s *Server) addFabrikaStatus(ticket *tickets.Ticket) {
	if s.config.Server.Database == "" || (ticket.Agent != "fabrika" && ticket.Agent != "nymo") {
		return
	}
	id := ticket.ID
	if len(id) != 18 || !strings.HasPrefix(id, "t_") {
		return
	}
	if _, err := hex.DecodeString(id[2:]); err != nil {
		return
	}
	root := filepath.Dir(s.config.Server.Database)
	switch phase := strings.TrimSpace(readFabrikaTail(filepath.Join(root, "model-stages", id), 32)); phase {
	case "spec", "build", "review", "approver":
		ticket.FabrikaStage = phase
	}
	for _, line := range strings.Split(readFabrikaTail(filepath.Join(root, "model-fallbacks", id), 16<<10), "\n") {
		if text, ok := strings.CutPrefix(line, "▸ Fallback "); ok {
			ticket.FabrikaFallbacks = append(ticket.FabrikaFallbacks, text)
		}
	}
	if count := len(ticket.FabrikaFallbacks); count > 20 {
		ticket.FabrikaFallbacks = ticket.FabrikaFallbacks[count-20:]
	}
}

func readFabrikaTail(path string, limit int64) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	skip := info.Size() > limit
	if skip {
		if _, err := file.Seek(info.Size()-limit, io.SeekStart); err != nil {
			return ""
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return ""
	}
	text := string(data)
	if skip {
		_, text, _ = strings.Cut(text, "\n")
	}
	return text
}
