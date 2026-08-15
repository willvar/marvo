import { tool } from "@opencode-ai/plugin";
import { callMarvo } from "../marvo-tool";

export default tool({
  description:
    "只读查询当前 Marvo 空间的历史会话。search 按标题查找候选；read 读取指定会话经过安全过滤的普通对话文本和附件名称。当前请求自足时不要调用。",
  args: {
    action: tool.schema.enum(["search", "read"]),
    query: tool.schema
      .string()
      .max(200)
      .optional()
      .describe("search 的标题关键词；省略时列出最近会话"),
    session_id: tool.schema
      .string()
      .max(128)
      .optional()
      .describe("read 所需的会话 ID"),
    limit: tool.schema
      .number()
      .int()
      .min(1)
      .max(100)
      .optional()
      .describe("最多返回或读取 100 条"),
  },
  async execute(args) {
    return callMarvo("sessions", args);
  },
});
