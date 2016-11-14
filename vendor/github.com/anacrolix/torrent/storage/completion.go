package storage

import (
	"github.com/anacrolix/torrent/metainfo"
)

type PieceCompletionType int

const (
	PieceCompletionSqlite = iota + 1
	PieceCompletionMmap
)

type pieceCompletion interface {
	Get(metainfo.Piece) (bool, error)
	Set(metainfo.Piece, bool) error
	Close()
}

func pieceCompletionForDir(dir string, t PieceCompletionType) (ret pieceCompletion) {
	var err error

	if t == PieceCompletionSqlite {
		ret, err = newDBPieceCompletion(dir)
		if err != nil {
			//log.Printf("couldn't open piece completion db in %q: %s", dir, err)
			ret = new(mapPieceCompletion)
		}
	} else {
		ret = new(mapPieceCompletion)
	}
	return
}
