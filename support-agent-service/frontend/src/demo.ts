import type { Conversation, DemoReply } from "./types.ts";

export const starters = [
  {
    id: "recent",
    icon: "layers",
    title: "学会使用 Workflow",
    detail: "了解创建、保存与运行的区别",
    prompt: "Workflow 是什么，如何创建和运行？",
  },
  {
    id: "visibility",
    icon: "eye",
    title: "弄清内容的可见性",
    detail: "了解公开、私密与受限的区别",
    prompt: "请解释内容可见性的几种状态，以及如何查看自己的内容。",
  },
  {
    id: "help",
    icon: "sparkles",
    title: "了解小栖能做什么",
    detail: "找到适合你的提问方式",
    prompt: "你可以帮我解决哪些问题？我应该怎么提问？",
  },
] as const;
export type SampleId = (typeof starters)[number]["id"];

export function demoReply(sample?: SampleId): DemoReply {
  if (sample === "recent")
    return {
      status: "answered",
      actions: ["保存和发布给 Agent 有什么区别？", "如何查看运行记录？"],
      answer:
        "这是界面演示说明：Workflow 是可复用的步骤流程。进入工作流定制页面，添加节点、连接并配置，保存成功后再运行测试。\n\n实际操作请以当前项目已发布指南和运行返回结果为准，小栖不会替你执行工作流。",
    };
  if (sample === "visibility")
    return {
      status: "answered",
      actions: ["站内 AI 助手在哪里，如何使用？"],
      answer:
        "内容可见性描述一条内容允许谁查看。公开、仅关注者可见、私密和受限等状态应以项目规则为准。\n\n这是一段演示说明。小栖解释使用方法；实际内容查询或分析，请前往站内 AI 助手继续尝试，以其当前可用能力为准。",
    };
  if (sample === "help")
    return {
      status: "answered",
      actions: [starters[0].prompt],
      answer:
        "小栖只做产品使用向导。你可以询问功能、规则，以及 Workflow 如何创建、保存和运行。\n\n内容检索、站内分析或创作任务，请前往站内 AI 助手继续尝试；小栖不会直接执行这些任务，也不保证其他助手当前具备全部能力。",
    };
  return {
    status: "partial",
    actions: [starters[0].prompt, starters[2].prompt],
    answer:
      "你的问题已经收到。这里是交互演示，当前没有调用模型或查询账号，因此不会生成针对这条问题的真实结论。\n\n你可以选择下方示例继续体验，或在右上角“连接账号”后，向真实助手发送这条问题。",
  };
}

export function sampleConversations(): Conversation[] {
  return starters.slice(0, 2).map((starter, index) => ({
    id: `sample-${starter.id}`,
    title: starter.title,
    sample: true,
    updatedAt: new Date().toISOString(),
    messages: [
      {
        id: `sample-user-${index}`,
        role: "user",
        text: starter.prompt,
        createdAt: new Date().toISOString(),
      },
      {
        id: `sample-answer-${index}`,
        role: "assistant",
        text: demoReply(starter.id).answer,
        status: "answered",
        actions: demoReply(starter.id).actions,
        createdAt: new Date().toISOString(),
      },
    ],
  }));
}
