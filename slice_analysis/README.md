# slice 源码分析

## 先看结论

slice的结构非常简单

- array: 指向array数组的切片
- len: 数组当前存储数据的个数，作为slice的长度
- cap: 数组的总长度，作为slice的容量

slice主要有三个函数调用

- makeslice: 计算总内存大小，分配内容，创建切片
- growslice: 扩容切片，
  - 新容量变成所需最小容量: 所需最小容量大于两倍旧容量
  - 新容量变成旧容量的两倍: 旧容量 < 256
  - 旧容量的2-1.25倍: 旧容量 > 256
- slicecopy: 从from区域拷贝到to区域，拷贝大小是两者中len的最小值
  - len = 0: 不拷贝
  - len = 1: 等号赋值
  - len > 1: 使用memmove复制内存

内置函数append

- 内置函数真正的名称叫做预定义标识符
- append最终被编译器转化为中间代码，会调用growslice、makeslice。有两种生成的代码逻辑
  - `append(s, e1, e2, e3)`: 如果新的len大于目前的cap则调用growslice扩容，然后依次赋值到对应索引位置
  - `s = append(s, e1, e2, e3)`: 如果新的len大于目前的cap则调用growslice扩容，然后依次赋值到对应索引位置。注意还需要对源变量s的ptr、len、cap重新赋值

slice截取

- 截取操作也是由中间代码生成的
- 经过查看确实是指向原来的数组，重新设置了cap、len，返回一个slice
- 对于slice、string、array的截取都将返回一个slice

## 基本示例

```go
func main() {
	slice := make([]int, 0, 3)
	slice = append(slice, 1)
	slice = append(slice, 2)
	slice = append(slice, 3)
	slice = append(slice, 4)
	slice = append(slice, 5)
	slice2 := []int{0, 0}
	copy(slice2, slice)
	fmt.Println(slice, len(slice), cap(slice))
	fmt.Println(slice2, len(slice2), cap(slice2))
}
```

这一段代码基本能涉及到slice的底层操作

翻译汇编指令`go tool compile -N -l -S main.go`，找出runtime执行函数，接着找出源码位置

```log
rel 48+4 t=9 **runtime.makeslice**+0
rel 168+8 t=3 type.int+0
rel 176+4 t=9 **runtime.growslice**+0
rel 284+8 t=3 type.[2]int+0
rel 292+4 t=9 runtime.newobject+0
rel 464+4 t=9 runtime.memmove+0
rel 524+4 t=9 **runtime.convTslice**+0
rel 540+8 t=3 type.[]int+0
rel 556+8 t=3 runtime.writeBarrier+0
rel 588+4 t=9 runtime.gcWriteBarrier+0
rel 600+4 t=9 runtime.convT64+0
rel 616+8 t=3 type.int+0
rel 632+8 t=3 runtime.writeBarrier+0
rel 664+4 t=9 runtime.gcWriteBarrier+0
rel 676+4 t=9 runtime.convT64+0
rel 692+8 t=3 type.int+0
rel 708+8 t=3 runtime.writeBarrier+0
rel 740+4 t=9 runtime.gcWriteBarrier+0
rel 780+4 t=9 fmt.Println+0
rel 832+4 t=9 runtime.convTslice+0
rel 848+8 t=3 type.[]int+0
rel 864+8 t=3 runtime.writeBarrier+0
rel 896+4 t=9 runtime.gcWriteBarrier+0
rel 908+4 t=9 runtime.convT64+0
rel 924+8 t=3 type.int+0
rel 940+8 t=3 runtime.writeBarrier+0
rel 972+4 t=9 runtime.gcWriteBarrier+0
rel 984+4 t=9 runtime.convT64+0
rel 1000+8 t=3 type.int+0
rel 1016+8 t=3 runtime.writeBarrier+0
rel 1048+4 t=9 runtime.gcWriteBarrier+0
rel 1088+4 t=9 fmt.Println+0
rel 1112+4 t=9 **runtime.panicSliceB**+0
rel 1124+4 t=9 runtime.morestack_noctxt+0
```

我们可用看出有以下几种调用

- runtime.makeslice
- runtime.growslice
- runtime.convTslice
- runtime.panicSliceB

可用通过在Go SDK全局搜索makeslice查找出slice源码的位置`runtime/slice.go`
在源码中我们可以看到主要有三个函数

- runtime.makeslice：创建切片
- runtime.growslice：扩容切片
- runtime.slicecopy：拷贝切片

看到这里我们肯定有好多个疑问：

- 对于append操作和截取操作怎么没有看到具体实现？
- 没有看到slicecopy在汇编指令中呀？
  我将在分析完`runtime/slice.go`代码后解答疑问

## 基本结构

```go
// 结构很简单，就只有三个字段
type slice struct {
	array unsafe.Pointer    // 指向array数组的指针
	len   int               // array数组当前元素数量
	cap   int               // array总容量
}
```

## 创建切片

```go
func makeslice(et *_type, len, cap int) unsafe.Pointer {
  // 计算出总大小 mem = et.size * cap
  mem, overflow := math.MulUintptr(et.size, uintptr(cap))
  // 如果溢出
  if overflow || mem > maxAlloc || len < 0 || len > cap {
      mem, overflow := math.MulUintptr(et.size, uintptr(len))
      if overflow || mem > maxAlloc || len < 0 {
          panicmakeslicelen()
      }
      // 报panic
      panicmakeslicecap()
  }
  // 使用mallocgc分配mem大小的内存，返回指针
  return mallocgc(mem, et, true)
}
```

创建切片总结：

1. 如果创建的总内存大小是否溢出或过大，直接panic
2. 使用mallocgc分配内存，返回指针

## 切片扩容

我们看它的说明

```log
// growslice handles slice growth during append.
// It is passed the slice element type, the old slice, and the desired new minimum capacity,
// and it returns a new slice with at least that capacity, with the old data
// copied into it.
// The new slice's length is set to the old slice's length,
// NOT to the new requested capacity.
// This is for codegen convenience. The old slice's length is used immediately
// to calculate where to write new values during an append.
// TODO: When the old backend is gone, reconsider this decision.
// The SSA backend might prefer the new length or to return only ptr/cap and save stack space.
```

chatGPT的解释：
这段注释是关于 Go 语言中的切片（slice）扩容（growth）操作的说明。我会逐句解释这段注释的内容：

1. growslice 是一个处理切片扩容的函数，它在进行 append 操作时被调用。
2. 这个函数会接收切片的元素类型、旧的切片和所需的新最小容量作为参数。
3. 它会返回一个新的切片，至少具有传入的最小容量，并且将旧数据复制到其中。
4. 新切片的长度被设置为旧切片的长度，而不是新请求的容量。这是为了在进行 append 操作时方便代码生成。旧切片的长度被立即使用来计算在追加新值时要写入的位置。
5. "TODO" 表示在以前的编译器后端被移除后，可能会重新考虑这个决定。可能在 SSA（静态单赋值）后端中，会更倾向于返回新的长度，或者仅返回指针/容量，以节省堆栈空间。

总之，这段注释描述了在进行切片扩容操作时的一些技术细节，包括如何处理新旧切片的长度和容量，以及为什么在生成代码时使用旧切片的长度来确定新值的写入位置。还提到了在后续的编译器开发中，可能会重新考虑如何处理这些细节。

```go
func growslice(et *_type, old slice, cap int) slice {
  if cap < old.cap {
      panic(errorString("growslice: cap out of range"))
  }
      // 元素类型大小为0，比如struct{} 直接返回
  if et.size == 0 {
      // append should not create a slice with nil pointer but non-zero len.
      // We assume that append doesn't need to preserve old.array in this case.
      return slice{unsafe.Pointer(&zerobase), old.len, cap}
  }
      // 新的容量先设置为旧容量的大小
  newcap := old.cap
  // 两倍的旧容量大小
  doublecap := newcap + newcap
  if cap > doublecap {
              // 如果所需的新最小容量大于两倍的旧容量
      // 新的容量被设置为所需的新最小容量
      newcap = cap
  } else {
      // 如果所需的新最小容量小于两倍的旧容量
      // 定义阈值256，为什么不在外部定义？？？
      const threshold = 256
      if old.cap < threshold {
                      // 如果旧容量小于256，新容量就等于两倍的旧容量
          newcap = doublecap
      } else {
          // 如果旧容量大于等于256
          // 以100%-25%递减的速度进行增长
          // 当newcap = 256时是100%大小增长
          // 当newcap越来越大，是无限接近25%的大小增长，但永远不可能达到25%。
          // 因为固定要加上196
          // 相当于每次相加newcap/4 + 196
          for 0 < newcap && newcap < cap {
              newcap += (newcap + 3*threshold) / 4
          }
          // 当newcap溢出时会变为负数，所有直接旧设置为所需的新最小容量
          if newcap <= 0 {
              newcap = cap
          }
      }
  }
      // 这一段代码的主要作用是判断新总大小是否溢出
  var overflow bool
  var lenmem, newlenmem, capmem uintptr
  switch {
  case et.size == 1:
      lenmem = uintptr(old.len)
      newlenmem = uintptr(cap)
      capmem = roundupsize(uintptr(newcap))
      overflow = uintptr(newcap) > maxAlloc
      newcap = int(capmem)
  case et.size == goarch.PtrSize:
      lenmem = uintptr(old.len) * goarch.PtrSize
      newlenmem = uintptr(cap) * goarch.PtrSize
      capmem = roundupsize(uintptr(newcap) * goarch.PtrSize)
      overflow = uintptr(newcap) > maxAlloc/goarch.PtrSize
      newcap = int(capmem / goarch.PtrSize)
  case isPowerOfTwo(et.size):
      var shift uintptr
      if goarch.PtrSize == 8 {
          // Mask shift for better code generation.
          shift = uintptr(sys.Ctz64(uint64(et.size))) & 63
      } else {
          shift = uintptr(sys.Ctz32(uint32(et.size))) & 31
      }
      lenmem = uintptr(old.len) << shift
      newlenmem = uintptr(cap) << shift
      capmem = roundupsize(uintptr(newcap) << shift)
      overflow = uintptr(newcap) > (maxAlloc >> shift)
      newcap = int(capmem >> shift)
  default:
      lenmem = uintptr(old.len) * et.size
      newlenmem = uintptr(cap) * et.size
      capmem, overflow = math.MulUintptr(et.size, uintptr(newcap))
      capmem = roundupsize(capmem)
      newcap = int(capmem / et.size)
  }
  // 溢出直接报panic
  if overflow || capmem > maxAlloc {
      panic(errorString("growslice: cap out of range"))
  }

  var p unsafe.Pointer
  // 分配新的内存
  if et.ptrdata == 0 {
      p = mallocgc(capmem, nil, false)
      // 清空len-cap这段区间的内存
      memclrNoHeapPointers(add(p, newlenmem), capmem-newlenmem)
  } else {
      p = mallocgc(capmem, et, true)
      if lenmem > 0 && writeBarrier.enabled {
          bulkBarrierPreWriteSrcOnly(uintptr(p), uintptr(old.array), lenmem-et.size+et.ptrdata)
      }
  }
  // 执行拷贝操作，将旧的数组拷贝到新的内存中。这也是copy函数底层实现函数
  memmove(p, old.array, lenmem)
  return slice{p, old.len, newcap}
}
```

切片扩容总结：
1. 如果新最小容量大于两倍旧容量，扩容为新最小容量
2. 新最小容量小于两倍旧容量，对于旧容量小于256，扩容为两倍旧容量；对于旧容量大于等于256，以200%到125%的大小进行扩容，容量越大，越接近125%
3. 如果新的内存大小是否过大或溢出，直接panic:cap out of range，否则正常分配新的内存，使用memmove将旧数组的内存值拷贝到新数组

## 切片复制

```go
// width参数的意思是元素大小
func slicecopy(toPtr unsafe.Pointer, toLen int, fromPtr unsafe.Pointer, fromLen int, width uintptr) int {
  // 如果有一方len为0，则直接返回不用copy
  if fromLen == 0 || toLen == 0 {
      return 0
  }

  // n的值是toLen和fromLen中较小的那个
  n := fromLen
  if toLen < n {
      n = toLen
  }
  // 如果元素大小为0，则不必复制
  if width == 0 {
      return n
  }
  // 需要复制的总大小
  size := uintptr(n) * width

  if size == 1 {
      // 只复制一个字节，不必使用memmove复制到目的地址，直接赋值就行了
      *(*byte)(toPtr) = *(*byte)(fromPtr)
  } else {
      // 内存复制操作从fromPtr地址处复制size长度的大小到toPtr当中
      memmove(toPtr, fromPtr, size)
  }
  // 返回复制元素的个数
  return n
}

```

切片复制总结：
切片复制是将from区域的内存复制到to区域，复制的长度是两者len的较小值，调用memmove来实现；如果复制的大小内存只有1字节，那么采用直接等号赋值的方式

## append内置函数

append函数不需要引入包，是go语言的内置函数，在点进去后发现只定义了函数签名，并没有任何代码的实现，不禁让我们好奇，究竟是如何实现的呢？

```
// The append built-in function appends elements to the end of a slice. If
// it has sufficient capacity, the destination is resliced to accommodate the
// new elements. If it does not, a new underlying array will be allocated.
// Append returns the updated slice. It is therefore necessary to store the
// result of append, often in the variable holding the slice itself:
//
//	slice = append(slice, elem1, elem2)
//	slice = append(slice, anotherSlice...)
//
// As a special case, it is legal to append a string to a byte slice, like this:
//
//	slice = append([]byte("hello "), "world"...)
func append(slice []Type, elems ...Type) []Type
```

> 注释翻译：
> append内置函数用于将元素追加到切片的末尾，如果切片任具有足够的容量，则会在切片上进行操作。
> 如果容量不足，则会分配新的底层数组。
> append函数返回更新后的切片。因此通常需要将返回值存储在持有切片的变量中。

### 预定义标识符

让我们来看看`builtin/builtin.go`是干什么的

```go
/*
Package builtin provides documentation for Go's predeclared identifiers.
The items documented here are not actually in package builtin
but their descriptions here allow godoc to present documentation
for the language's special identifiers.
*/
package builtin

type uint8 uint8
type uint16 uint16
type uint32 uint32
type uint64 uint64
// ......
type any = interface{}
// ......
func append(slice []Type, elems ...Type) []Type
func copy(dst, src []Type) int
func delete(m map[Type]Type1, key Type)
func len(v Type) int
func cap(v Type) int
// ......
```

> 包 "builtin" 提供了 Go 语言预声明标识符的文档说明。这里所记录的项目实际上并不在 "builtin" 包中，但是它们在这里的描述使得 godoc 能够为语言中的特殊标识符提供文档展示。

我们可以看到 builtin 用来定义预定义标识符的，什么是预定义标识符。
预定义标识符与continue、select、range这些关键字不一样。这些标识符拥有全局的作用域，可以在任意源码位置使用。
其中我们发现bool、int、float、any、len()、cap()、append()等都是预定义标识符
我们知道了内建函数如copy、append的真正名称叫做**预定义标识符**，只不过这些标识符可以用作函数使用。

### append中间代码

append内建函数的实现源码在哪里？
其实我们查看了上面的汇编指令就可发现，append内建函数并没有以独立的身份出现在汇编指令中，而是被转化为runtime.growslice、runtime.copyslice等这些函数调用

这些其实都是编译器做的，go语言编译可以分成四个阶段：

1. 词法语法分析
2. 类型检查、抽象语法数生成
3. 中间代码生成
4. 生成最终的机器码

可以关注第三阶段，看看有没有关于append标识符被转化的中间代码实现

经过不懈努力和搜索，我找到了append函数中间生成源码 `/src/cmd/compile/internal/ssagen/ssa.go`
关于这段代码就不进行分析了，能力有限，我们大概看一下转换后的代码也就是注释的代码

```go
// append converts an OAPPEND node to SSA.
// If inplace is false, it converts the OAPPEND expression n to an ssa.Value,
// adds it to s, and returns the Value.
// If inplace is true, it writes the result of the OAPPEND expression n
// back to the slice being appended to, and returns nil.
// inplace MUST be set to false if the slice can be SSA'd.
func (s *state) append(n *ir.CallExpr, inplace bool) *ssa.Value {
  // If inplace is false, process as expression "append(s, e1, e2, e3)":
  //
  // ptr, len, cap := s
  // newlen := len + 3
  // if newlen > cap {
  //     ptr, len, cap = growslice(s, newlen)
  //     newlen = len + 3 // recalculate to avoid a spill
  // }
  // // with write barriers, if needed:
  // *(ptr+len) = e1
  // *(ptr+len+1) = e2
  // *(ptr+len+2) = e3
  // return makeslice(ptr, newlen, cap)
  //
  //
  // If inplace is true, process as statement "s = append(s, e1, e2, e3)":
  //
  // a := &s
  // ptr, len, cap := s
  // newlen := len + 3
  // if uint(newlen) > uint(cap) {
  //    newptr, len, newcap = growslice(ptr, len, cap, newlen)
  //    vardef(a)       // if necessary, advise liveness we are writing a new a
  //    *a.cap = newcap // write before ptr to avoid a spill
  //    *a.ptr = newptr // with write barrier
  // }
  // newlen = len + 3 // recalculate to avoid a spill
  // *a.len = newlen
  // // with write barriers, if needed:
  // *(ptr+len) = e1
  // *(ptr+len+1) = e2
  // *(ptr+len+2) = e3

  // ......
}
```

总结一下append内置函数的逻辑：

1. `append(s, e1, e2, e3)`: 如果新的len大于目前的cap则调用growslice扩容，然后依次赋值到对应索引位置
2. `s = append(s, e1, e2, e3)`: 如果新的len大于目前的cap则调用growslice扩容，然后依次赋值到对应索引位置。注意还需要对源变量s的ptr、len、cap重新赋值

到这里append函数基本上就分析完了

## slice截取

`ssa.go`的代码为翻了好久终于找到了截取slice相关的实现

基本可以看到新切片是指向原来slice内存中的某个位置，然后要设置好len和cap

对slice、string、array的截取操作都将返回一个slice

```go
// slice computes the slice v[i:j:k] and returns ptr, len, and cap of result.
// i,j,k may be nil, in which case they are set to their default value.
// v may be a slice, string or pointer to an array.
func (s *state) slice(v, i, j, k *ssa.Value, bounded bool) (p, l, c *ssa.Value) {
  // ......
  // Calculate the base pointer (rptr) for the new slice.
  //
  // Generate the following code assuming that indexes are in bounds.
  // The masking is to make sure that we don't generate a slice
  // that points to the next object in memory. We cannot just set
  // the pointer to nil because then we would create a nil slice or
  // string.
  //
  //     rcap = k - i
  //     rlen = j - i
  //     rptr = ptr + (mask(rcap) & (i * stride))
  //
  // Where mask(x) is 0 if x==0 and -1 if x>0 and stride is the width
  // of the element type.
}
```

## runtime.convTslice

在上面的汇编代码中还有runtime.convTslice调用没有解释：
这个函数是定义在`iface.go`当中的，看样子跟接口有点关系
因为这个函数跟切片的整体逻辑关系不大，就先不重点分析了，我先用chatGPT简单分析一下这个调用

```go
func convTslice(val []byte) (x unsafe.Pointer) {
  // Note: this must work for any element type, not just byte.
  if (*slice)(unsafe.Pointer(&val)).array == nil {
      x = unsafe.Pointer(&zeroVal[0])
  } else {
      x = mallocgc(unsafe.Sizeof(val), sliceType, true)
      *(*[]byte)(x) = val
  }
  return
}
```

chatGPT：
这个函数是 Go 语言运行时库中的一部分，用于将一个字节切片（`[]byte`）转换为一个 `unsafe.Pointer`。它不仅适用于字节切片，还适用于任何元素类型的切片。该函数的目的是将切片转换为不受类型限制的指针。

具体来说，这个函数 `convTslice` 的功能如下：

1. 首先，它通过将传入的字节切片的指针转换为 `*slice` 类型的指针，来获取切片的底层数组指针和长度信息。这是通过使用 `unsafe.Pointer` 进行指针转换来实现的。这里使用 `*slice` 是因为切片的底层数组指针和长度信息都包含在 `slice` 结构体中。
2. 接着，它检查切片的底层数组指针是否为 `nil`。如果底层数组指针为 `nil`，说明传入的切片是一个空切片，它将 `x` 设置为指向预先定义的 `zeroVal[0]`。`zeroVal` 是一个包含零值的数组，用于表示空切片的底层数据。
3. 如果底层数组指针不为 `nil`，则说明传入的切片不为空，它将使用 `mallocgc` 函数为这个切片分配内存。`mallocgc` 是运行时系统用于分配内存的函数，它接收分配的内存大小、类型和一个布尔值参数，用于指示是否进行垃圾回收。
4. 最后，函数将传入的切片内容复制到新分配的内存中，从而将切片转换为一个不受类型限制的 `unsafe.Pointer`。这里通过将 `*[]byte` 类型的指针转换为 `[]byte` 类型的指针，然后通过解引用将切片内容复制到新分配的内存中。
   总之，`convTslice` 函数的作用是将一个切片转换为一个不受类型限制的指针，用于在需要时绕过 Go 语言的类型系统进行底层操作。这种操作一般需要谨慎处理，因为它可能会导致类型安全性问题和内存管理问题。

## runtime.panicSliceB

当出现slice严重错误时运行，不再多说
