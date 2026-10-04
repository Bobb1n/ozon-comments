package graphql

import "ozon/internal/transport/graphql/generated"

func pageComplexity(pageLimit, budget int) generated.ComplexityRoot {
	var complexity generated.ComplexityRoot
	cost := func(children, first int) int {
		if first < 1 || first > pageLimit {
			return 1
		}
		children = max(1, children)
		if children > (budget-1)/first {
			return budget + 1
		}
		return 1 + first*children
	}
	complexity.Query.Posts = func(children, first int, _ *string) int { return cost(children, first) }
	complexity.Query.Comments = func(children int, _ string, _ *string, first int, _ *string) int { return cost(children, first) }
	return complexity
}
