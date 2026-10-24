package fw

import (
	"database/sql"
	"errors"
	"fmt"
)

// PairOpen says whether new connections from network a to network b are allowed by the network
// types alone, before any exception. It mirrors what the compiler builds.
func PairOpen(a, b Zone) bool {
	switch {
	case a.Kind == KindWan:
		return false
	case a.Name == b.Name:
		return a.IntraAllow
	case b.Kind == KindWan:
		return a.Internet && a.Gateway
	case a.Kind == KindTrusted && a.Gateway:
		return true
	}
	return false
}

// Matrix modes.
const (
	MatrixAllow   = "allow"   // every new connection from the first network to the second is allowed
	MatrixBlock   = "block"   // none is
	MatrixReplies = "replies" // none is, but the second network may start connections to the first
	MatrixDefault = "default" // back to what the network types say
)

// MatrixPrefix starts the name of every rule made by the matrix.
const MatrixPrefix = "Matrix "

func matrixName(from, to string) string { return MatrixPrefix + shortHash(from+">"+to) }

// IsMatrixRule says whether a rule was made by the matrix.
func IsMatrixRule(name string) bool {
	return len(name) > len(MatrixPrefix) && name[:len(MatrixPrefix)] == MatrixPrefix
}

func putMatrixRule(tx *sql.Tx, from, to, action, why string) error {
	name := matrixName(from, to)
	var id int64
	err := tx.QueryRow(`SELECT id FROM fw_rules WHERE from_zone=? AND to_zone=? AND name=?`, from, to, name).Scan(&id)
	r := RuleRow{ID: id, Name: name, From: from, To: to, Service: "any", Action: action, Enabled: true, Descr: why}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = SaveRule(tx, r)
	return err
}

func dropMatrixRule(tx *sql.Tx, from, to string) {
	tx.Exec(`DELETE FROM fw_rules WHERE from_zone=? AND to_zone=? AND name=?`, from, to, matrixName(from, to))
}

// SetMatrix changes how one pair of networks is treated, using rules the matrix owns. Rules the
// person wrote by hand are not touched, and the built-in protected rules always stay above.
func SetMatrix(tx *sql.Tx, zones []Zone, from, to, mode string) error {
	byName := map[string]Zone{}
	for _, z := range zones {
		byName[z.Name] = z
	}
	a, okA := byName[from]
	b, okB := byName[to]
	if !okA || !okB {
		return errors.New("choose two of your networks")
	}
	if from == to {
		return errors.New("whether devices in one network can reach each other is set when you edit the network")
	}
	if a.Kind == KindWan && mode != MatrixDefault && mode != MatrixBlock {
		return errors.New("to let the internet reach a device, open a port to it instead")
	}
	why := fmt.Sprintf("Set from the matrix: %s to %s", from, to)
	switch mode {
	case MatrixDefault:
		dropMatrixRule(tx, from, to)
	case MatrixAllow:
		return putMatrixRule(tx, from, to, "accept", why+", allow all")
	case MatrixBlock:
		return putMatrixRule(tx, from, to, "drop", why+", block all")
	case MatrixReplies:
		if a.Kind == KindWan || b.Kind == KindWan {
			return errors.New("\"replies only\" is for two of your own networks")
		}
		if err := putMatrixRule(tx, from, to, "drop", why+", replies only"); err != nil {
			return err
		}
		if !PairOpen(b, a) {
			return putMatrixRule(tx, to, from, "accept", fmt.Sprintf("Set from the matrix: %s to %s, so replies can come back", to, from))
		}
		dropMatrixRule(tx, to, from)
	default:
		return errors.New("unknown choice")
	}
	return nil
}
