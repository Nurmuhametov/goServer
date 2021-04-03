package server

type FuncStack struct {
	f    func(str string, client *ConnectedClient)
	next *FuncStack
}

func funcPush(s **FuncStack, f func(string2 string, client *ConnectedClient)) {
	var newRoot = new(FuncStack)
	*newRoot = FuncStack{
		f:    f,
		next: *s,
	}
	*s = newRoot
}

func funcPop(stack **FuncStack) (func(string2 string, client *ConnectedClient), bool) {
	if stack == nil {
		return nil, false
	}
	var temp = *stack
	*stack = (*stack).next
	return temp.f, true
}

func funcPeek(stack *FuncStack) func(string2 string, client *ConnectedClient) {
	if stack == nil {
		panic("attempt to funcPeek from empty func stack")
	}
	return stack.f
}

type PositionStack struct {
	f    [2]uint8
	next *PositionStack
}

func posPush(stack **PositionStack, position [2]uint8) {
	var newRoot = new(PositionStack)
	*newRoot = PositionStack{
		f:    position,
		next: *stack,
	}
	*stack = newRoot
}

func posPop(stack **PositionStack) ([2]uint8, bool) {
	if stack == nil {
		return [2]uint8{}, false
	}
	var temp = *stack
	*stack = (*stack).next
	return temp.f, true
}

func posPeek(stack *PositionStack) [2]uint8 {
	if stack == nil {
		panic("attempt to funcPeek from empty pos stack")
	}
	return stack.f
}
