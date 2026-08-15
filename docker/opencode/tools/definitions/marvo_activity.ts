import { tool } from "@opencode-ai/plugin";
import { callMarvo } from "../marvo-tool";

export default tool({
  description:
    "向用户的 Marvo 活动流发布一条主动消息。普通结果或提醒使用 notice；需要用户选择时使用 choice，且 choice 始终允许用户补充自己的想法。活动应当自足且不暴露内部信息；发布成功后只需在当前对话简短确认，不要重复正文。",
  args: {
    kind: tool.schema.enum(["notice", "choice"]).describe("活动类型"),
    title: tool.schema.string().min(1).max(200).describe("简短标题"),
    content: tool.schema
      .string()
      .min(1)
      .max(65536)
      .describe(
        "给用户看的完整内容，支持 Markdown；基于外部资料时必须附上可直接访问的原始来源链接",
      ),
    choices: tool.schema
      .array(tool.schema.string().min(1).max(200))
      .max(20)
      .optional()
      .describe("choice 的可选项，至少两个；notice 不要传入"),
    multiple: tool.schema
      .boolean()
      .optional()
      .describe("choice 是否允许多选；省略时为单选"),
  },
  async execute(args, context) {
    return callMarvo("activity", {
      ...args,
      source_session_id: context.sessionID,
      source_message_id: context.messageID,
    });
  },
});
