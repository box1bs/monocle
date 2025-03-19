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
	lenght := min(len(d.Words), 256)
	d.PartOfFullSize = 256.0 / float64(len(d.Words))
	d.Words = d.Words[:lenght]
}

func (d *Document) GetFullSize() float64 {
	return float64(256.0 / d.PartOfFullSize)
}