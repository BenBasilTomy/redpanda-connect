// Copyright 2024 Redpanda Data, Inc.
//
// Licensed as a Redpanda Enterprise file under the Redpanda Community
// License (the "License"); you may not use this file except in compliance with
// the License. You may obtain a copy of the License at
//
// https://github.com/redpanda-data/connect/blob/main/licenses/rcl.md

package ollama

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/ollama/ollama/api"
	"github.com/redpanda-data/benthos/v4/public/service"
)

const (
	ocpFieldUserPrompt   = "prompt"
	ocpFieldSystemPrompt = "system_prompt"
)

func init() {
	err := service.RegisterProcessor(
		"ollama_chat",
		ollamaChatProcessorConfig(),
		makeOllamaCompletionProcessor,
	)
	if err != nil {
		panic(err)
	}
}

func ollamaChatProcessorConfig() *service.ConfigSpec {
	return service.NewConfigSpec().
		Categories("AI").
		Summary("Generates responses to messages in a chat conversation, using the Ollama API.").
		Description(`This processor sends prompts to your chosen Ollama large language model (LLM) and generates text from the responses, using the Ollama API.

By default, the processor starts and runs a locally installed Ollama server. Alternatively, to use an already running Ollama server, add your server details to the `+"`"+bopFieldServerAddress+"`"+` field. You can https://ollama.com/download[download and install Ollama from the Ollama website^].

For more information, see the https://github.com/ollama/ollama/tree/main/docs[Ollama documentation^].`).
		Version("4.32.0").
		Fields(
			service.NewStringField(bopFieldServerAddress).
				Description("The address of the Ollama server to use. Leave the field blank and the processor starts and runs a local Ollama server or specify the address of your own local or remote server.").
				Example("http://127.0.0.1:11434").
				Optional(),
			service.NewStringField(bopFieldModel).
				Description("The name of the Ollama LLM to use. For a full list of models, see the https://ollama.com/models[Ollama website].").
				Examples("llama3", "gemma2", "qwen2", "phi3"),
			service.NewInterpolatedStringField(ocpFieldUserPrompt).
				Description("The prompt you want to generate a response for. By default, the processor submits the entire payload as a string.").
				Optional(),
			service.NewInterpolatedStringField(ocpFieldSystemPrompt).
				Description("The system prompt to submit to the Ollama LLM.").
				Advanced().
				Optional(),
		)
}

func makeOllamaCompletionProcessor(conf *service.ParsedConfig, mgr *service.Resources) (service.Processor, error) {
	p := ollamaCompletionProcessor{}
	if conf.Contains(ocpFieldUserPrompt) {
		pf, err := conf.FieldInterpolatedString(ocpFieldUserPrompt)
		if err != nil {
			return nil, err
		}
		p.userPrompt = pf
	}
	if conf.Contains(ocpFieldSystemPrompt) {
		pf, err := conf.FieldInterpolatedString(ocpFieldSystemPrompt)
		if err != nil {
			return nil, err
		}
		p.systemPrompt = pf
	}
	b, err := newBaseProcessor(conf, mgr)
	if err != nil {
		return nil, err
	}
	p.baseOllamaProcessor = b
	return &p, nil
}

type ollamaCompletionProcessor struct {
	*baseOllamaProcessor

	userPrompt   *service.InterpolatedString
	systemPrompt *service.InterpolatedString
}

func (o *ollamaCompletionProcessor) Process(ctx context.Context, msg *service.Message) (service.MessageBatch, error) {
	var sp string
	if o.systemPrompt != nil {
		p, err := o.systemPrompt.TryString(msg)
		if err != nil {
			return nil, err
		}
		sp = p
	}
	up, err := o.computePrompt(msg)
	if err != nil {
		return nil, err
	}
	g, err := o.generateCompletion(ctx, sp, up)
	if err != nil {
		return nil, err
	}
	m := msg.Copy()
	m.SetBytes([]byte(g))
	return service.MessageBatch{m}, nil
}

func (o *ollamaCompletionProcessor) computePrompt(msg *service.Message) (string, error) {
	if o.userPrompt != nil {
		return o.userPrompt.TryString(msg)
	}
	b, err := msg.AsBytes()
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", errors.New("message payload contained invalid UTF8")
	}
	return string(b), nil
}

func (o *ollamaCompletionProcessor) generateCompletion(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	var req api.ChatRequest
	req.Model = o.model
	if systemPrompt != "" {
		req.Messages = append(req.Messages, api.Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}
	req.Messages = append(req.Messages, api.Message{
		Role:    "user",
		Content: userPrompt,
	})
	shouldStream := false
	req.Stream = &shouldStream
	var g string
	err := o.client.Chat(ctx, &req, func(resp api.ChatResponse) error {
		g = resp.Message.Content
		return nil
	})
	return g, err
}

func (o *ollamaCompletionProcessor) Close(ctx context.Context) error {
	return o.baseOllamaProcessor.Close(ctx)
}
