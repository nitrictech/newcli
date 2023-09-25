package treetable

type Node[T any] struct {
	Data     T
	Children []*Node[T]
}

// Trees enforce a uniform data type
// this makes it easier to build trees using reflection
type Tree[T any] struct {
	Root *Node[T]
}
