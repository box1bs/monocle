package model

import "github.com/google/uuid"

type Document struct {
	Id 				uuid.UUID	`json:"id"`
	URL				string		`json:"url"`
	WordCount 		int			`json:"words_count"`
	PartOfFullSize	float64		`json:"part_of_full_size"`
	WordVec 		[][]float64	`json:"word_vec"`
	TitleVec 		[][]float64	`json:"title_vec"`
}

func (d *Document) GetFullSize() float64 {
	return float64(256.0 / d.PartOfFullSize)
}