package model

import "github.com/google/uuid"

type Document struct {
	Id      		uuid.UUID
	URL     		string
	Description 	string
	Words 			[]string
	PartOfFullSize 	float64
}

func (d *Document) ArchiveDocument() {
	d.PartOfFullSize = 256.0 / float64(len(d.Words))
	d.Words = d.Words[:256]
}

func (d *Document) GetFullSize() float64 {
	return float64(256.0 / d.PartOfFullSize)
}