package graphql

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"ozon/internal/domain"
	"ozon/internal/service"
	"ozon/internal/transport/dto"
	"ozon/internal/transport/graphql/model"
)

func decodePage(first int, after *string, scope string, pageLimit int) (domain.PageRequest, error) {
	page := domain.PageRequest{Limit: first}
	if after != nil {
		data, err := base64.RawURLEncoding.DecodeString(*after)
		if err != nil {
			return page, fmt.Errorf("%w: malformed cursor", domain.ErrInvalidInput)
		}
		var cursor dto.Cursor
		if json.Unmarshal(data, &cursor) != nil || cursor.Scope != scope {
			return page, fmt.Errorf("%w: cursor does not belong to this list", domain.ErrInvalidInput)
		}
		page.After = &domain.PageCursor{CreatedAt: cursor.CreatedAt, ID: cursor.ID}
	}
	return page, service.ValidatePage(page, pageLimit)
}

func encodeCursor(createdAt time.Time, id, scope string) string {
	data, err := json.Marshal(dto.Cursor{CreatedAt: createdAt, ID: id, Scope: scope})
	if err != nil {
		panic(fmt.Errorf("encode cursor: %w", err))
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func commentScope(postID string, parentID *string) string {
	data, _ := json.Marshal([]any{postID, parentID})
	return string(data)
}

func postPage(page domain.Page[domain.Post]) *model.PostPage {
	result := &model.PostPage{
		Items:       make([]*domain.Post, 0, len(page.Items)),
		HasNextPage: page.HasNext,
	}
	for _, post := range page.Items {
		result.Items = append(result.Items, &post)
	}
	if len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		cursor := encodeCursor(last.CreatedAt, last.ID, "posts")
		result.EndCursor = &cursor
	}
	return result
}

func commentPage(page domain.Page[domain.Comment], scope string) *model.CommentPage {
	result := &model.CommentPage{
		Items:       make([]*domain.Comment, 0, len(page.Items)),
		HasNextPage: page.HasNext,
	}
	for _, comment := range page.Items {
		result.Items = append(result.Items, &comment)
	}
	if len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		cursor := encodeCursor(last.CreatedAt, last.ID, scope)
		result.EndCursor = &cursor
	}
	return result
}
