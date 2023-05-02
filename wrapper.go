package rwkv

import (
	"errors"
	"unsafe"
	"strings"
)

/*
#include <stdlib.h>
*/
//#cgo CFLAGS: -I./rwkv.cpp/ggml/include/ggml/
//#cgo CPPFLAGS: -I./rwkv.cpp/ggml/include/ggml/
//#cgo LDFLAGS: -L${SRCDIR}  -lrwkv
// #include "includes.h"
import "C"


type Context struct {
	cCtx *C.struct_rwkv_context
}

func InitFromFile(modelFilePath string, nThreads uint32) (*Context, error) {
	cModelFilePath := C.CString(modelFilePath)
	defer C.free(unsafe.Pointer(cModelFilePath))

	cCtx := C.rwkv_init_from_file(cModelFilePath, C.uint32_t(nThreads))
	if cCtx == nil {
		return nil, errors.New("failed to initialize rwkv context from file")
	}

	return &Context{cCtx}, nil
}

func (ctx *Context) Eval(token int32, stateIn []float32) ([]float32, []float32, bool, error) {
	var cStateIn, cStateOut, cLogitsOut *C.float

	logitsOut := make([]float32, ctx.GetLogitsBufferElementCount())
	stateOut := make([]float32, ctx.GetStateBufferElementCount())

	if stateIn != nil {
		cStateIn = (*C.float)(unsafe.Pointer(&stateIn[0]))
	}

	if stateOut != nil {
		cStateOut = (*C.float)(unsafe.Pointer(&stateOut[0]))
	}

	if logitsOut != nil {
		cLogitsOut = (*C.float)(unsafe.Pointer(&logitsOut[0]))
	}

	success := C.rwkv_eval(ctx.cCtx, C.int32_t(token), cStateIn, cStateOut, cLogitsOut)
	if success == false {
		return nil, nil, false, errors.New("failed to evaluate rwkv")
	}

	return stateOut, logitsOut, true, nil
}

func (ctx *Context) GetStateBufferElementCount() uint32 {
	return uint32(C.rwkv_get_state_buffer_element_count(ctx.cCtx))
}

func (ctx *Context) GetLogitsBufferElementCount() uint32 {
	return uint32(C.rwkv_get_logits_buffer_element_count(ctx.cCtx))
}

func (ctx *Context) Free() {
	C.rwkv_free(ctx.cCtx)
	ctx.cCtx = nil
}

func QuantizeModelFile(modelFilePathIn, modelFilePathOut string, formatName string) (bool, error) {
	cModelFilePathIn := C.CString(modelFilePathIn)
	defer C.free(unsafe.Pointer(cModelFilePathIn))

	cModelFilePathOut := C.CString(modelFilePathOut)
	defer C.free(unsafe.Pointer(cModelFilePathOut))

	cFormatName := C.CString(formatName)
	defer C.free(unsafe.Pointer(cFormatName))

	success := C.rwkv_quantize_model_file(cModelFilePathIn, cModelFilePathOut, cFormatName)
	if success == false {
		return false, errors.New("failed to quantize the model file")
	}

	return true, nil
}

func GetSystemInfoString() string {
	return C.GoString(C.rwkv_get_system_info_string())
}



func process_input(input string, elem_buff []float32, tk *Tokenizer, ctx *Context) ([]float32, []float32, error) {
	tokens, err := tk.Encode(input)
	if err != nil {
		panic(err)
	}

	logit_buff := make([]float32, ctx.GetLogitsBufferElementCount())

	for _, t := range tokens {
		

		elem_buff, logit_buff, _, err = ctx.Eval(int32(t.ID), elem_buff)
		if err != nil {
			panic(err)
		}

	}

	return elem_buff, logit_buff, nil
}

type RwkvState struct {
	// The context
	Context   *Context
	State     []float32
	Logits    []float32
	Tokenizer *Tokenizer
}

// Loadfiles("aimodels/RWKV-4-Raven-1B5-v9-Eng99%-Other1%-20230411-ctx4096_quant4.bin", "rwkv.cpp/rwkv/20B_tokenizer.json", 8)

func LoadFiles(modelFile, tokenFile string, threads uint32) *RwkvState {
	ctx, err := InitFromFile(modelFile, threads)

	elem_size := ctx.GetStateBufferElementCount()
	logit_size := ctx.GetLogitsBufferElementCount()

	elem_buff := make([]float32, elem_size)
	logit_buff := make([]float32, logit_size)
	if err != nil {
		return nil
	}

	tk, err := LoadTokeniser(tokenFile)
	if err != nil {
		return nil
	}

	return &RwkvState{
		Context:   ctx,
		State:     elem_buff,
		Logits:    logit_buff,
		Tokenizer: &tk,
	}
}


func (r *RwkvState) ProcessInput(input string) error {
	elems, logites, err := process_input(input, r.State, r.Tokenizer, r.Context)
	if err != nil {
		return err
	}
	r.State = elems
	r.Logits = logites
	return nil
}

func (r *RwkvState) PredictNextToken() string {
	newtoken, err := sampleLogits(r.Logits, 0.2, 1, map[int]float32{})
	if err != nil {
		panic(err)
	}

	chars := DeTokenise(*r.Tokenizer, []int{newtoken})
	return chars
}

func (r *RwkvState) GenerateResponse(maxTokens int, stopString string) string {
	response_text := ""
	for i := 0; i < maxTokens; i++ {

		newtoken, err := sampleLogits(r.Logits, 0.2, 1, map[int]float32{})
		if err != nil {
			panic(err)
		}

		r.State, r.Logits, _, err = r.Context.Eval(int32(newtoken), r.State)
		if err != nil {
			panic(err)
		}

		chars := DeTokenise(*r.Tokenizer, []int{newtoken})
		response_text += chars

		if strings.Contains(response_text, stopString) {
			//Split on stopstring, and return the first part
			response_text = strings.Split(response_text, stopString)[0]
			break
		}
	}
	return response_text

}
