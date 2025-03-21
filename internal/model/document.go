package model

import "github.com/google/uuid"

type Document struct {
	Id      		uuid.UUID
	URL     		string
	Description 	string
	WordCount 		int
	PartOfFullSize 	float64
}

func (d *Document) GetFullSize() float64 {
	return float64(256.0 / d.PartOfFullSize)
}