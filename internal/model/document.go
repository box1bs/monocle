package model

import "github.com/google/uuid"

type Document struct {
	Id 				uuid.UUID	`json:"id"`
	URL				string		`json:"url"`
	Description		string		`json:"description"`
	WordCount 		int			`json:"words_count"`
	PartOfFullSize	float64		`json:"part_of_full_size"`
	Vec 			[]float64	`json:"vec"`
}

func (d *Document) GetFullSize() float64 {
	return float64(256.0 / d.PartOfFullSize)
}