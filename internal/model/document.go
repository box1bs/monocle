package model

import "github.com/google/uuid"

type Document struct {
	Id      		uuid.UUID
	URL     		string
	Description 	string
	Sequence 		[]int
	PartOfFullSize 	float64
}

func (d *Document) ArchiveDocument() {
	lenght := min(len(d.Sequence), 256)
	d.PartOfFullSize = 256.0 / float64(len(d.Sequence))
	d.Sequence = d.Sequence[:lenght]
}

func (d *Document) GetFullSize() float64 {
	return float64(256.0 / d.PartOfFullSize)
}