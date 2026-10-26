package service

import (
	"context"
	"errors"
	"testing"

	agentproject "twitter-clone/internal/module/agent/project"
	"twitter-clone/internal/module/agent/repository"
)

var _ SubjectWriteAuthorizer = (*repository.MongoAgentRepository)(nil)

type subjectWriteAuthorizerFake struct {
	userID        uint64
	err           error
	calls         int
	blockedUserID uint64
}

func (f *subjectWriteAuthorizerFake) AuthorizeSubjectWrite(_ context.Context, userID uint64) error {
	f.calls++
	f.userID = userID
	if f.blockedUserID != 0 && f.blockedUserID != userID {
		return nil
	}
	return f.err
}

func TestAgentServiceAuthorizeSubjectWriteDelegatesAndFailsClosed(t *testing.T) {
	blocked := errors.New("subject is fenced")
	authorizer := &subjectWriteAuthorizerFake{err: blocked}
	service := &AgentService{subjectWriteAuthorizer: authorizer}
	err := service.authorizeSubjectWrite(context.Background(), 42)
	if !errors.Is(err, blocked) || authorizer.calls != 1 || authorizer.userID != 42 {
		t.Fatalf("authorizeSubjectWrite() = %v, fake=%+v", err, authorizer)
	}
}

func TestAgentServiceAuthorizeSubjectWriteRejectsInvalidIdentity(t *testing.T) {
	service := &AgentService{}
	if err := service.authorizeSubjectWrite(context.Background(), 0); err == nil {
		t.Fatal("zero user id was accepted")
	}
}

func TestFencedSubjectCannotStartProjectOrCompensation(t *testing.T) {
	blocked := errors.New("subject is fenced")
	for _, action := range []string{"create-project", "retry-compensation"} {
		t.Run(action, func(t *testing.T) {
			authorizer := &subjectWriteAuthorizerFake{err: blocked}
			service := &AgentService{subjectWriteAuthorizer: authorizer}
			var err error
			if action == "create-project" {
				_, err = service.CreateAgentProject(context.Background(), 42, "project")
			} else {
				_, err = service.RetryWorkflowCompensation(context.Background(), 42, "run")
			}
			if !errors.Is(err, blocked) || authorizer.calls != 1 {
				t.Fatalf("fenced request reached dependencies: error=%v calls=%d", err, authorizer.calls)
			}
		})
	}
}

func TestFencedTargetCannotBeAddedToProject(t *testing.T) {
	blocked := errors.New("target is fenced")
	authorizer := &subjectWriteAuthorizerFake{err: blocked, blockedUserID: 42}
	service := &AgentService{
		subjectWriteAuthorizer: authorizer,
		agentProjectManager:    agentproject.NewManager(nil, nil),
	}
	_, err := service.UpsertAgentProjectMember(context.Background(), 7, "project", 42, "member", 1)
	if !errors.Is(err, blocked) || authorizer.calls != 2 || authorizer.userID != 42 {
		t.Fatalf("fenced target reached project manager: error=%v authorizer=%+v", err, authorizer)
	}
}
