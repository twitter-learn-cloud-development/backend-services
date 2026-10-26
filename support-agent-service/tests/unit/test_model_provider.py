from langchain_core.messages import AIMessage, HumanMessage

from app.integrations.model_provider import FakeModelProvider, ModelRole


def test_fake_model_provider_returns_scripted_content_and_tool_calls() -> None:
    provider = FakeModelProvider(
        {
            ModelRole.WORKER: [
                AIMessage(
                    content="",
                    tool_calls=[
                        {
                            "name": "platform.get_status",
                            "args": {},
                            "id": "call_1",
                            "type": "tool_call",
                        }
                    ],
                )
            ]
        }
    )

    response = provider.get_chat_model(ModelRole.WORKER).invoke(
        [HumanMessage(content="Check the platform status")]
    )

    assert response.tool_calls[0]["name"] == "platform.get_status"
    assert response.tool_calls[0]["id"] == "call_1"
