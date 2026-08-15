import { tool } from "@opencode-ai/plugin";
import { callMarvo } from "../marvo-tool";

export default tool({
  description:
    "读取或修改当前用户的 Marvo 智能体设置。仅按用户明确要求修改；模型和推理强度从下一次消息起生效，全局提示词可能等待当前任务结束后生效。",
  args: {
    action: tool.schema.enum(["get", "update"]),
    provider_id: tool.schema
      .string()
      .max(256)
      .optional()
      .describe("与 model_id 一起更新"),
    model_id: tool.schema
      .string()
      .max(512)
      .optional()
      .describe("与 provider_id 一起更新"),
    variant: tool.schema
      .string()
      .max(256)
      .optional()
      .describe("推理强度；空字符串表示清除"),
    global_prompt: tool.schema
      .string()
      .max(65536)
      .optional()
      .describe("全局提示词；空字符串表示清除"),
    clear_model: tool.schema
      .boolean()
      .optional()
      .describe("清除 Marvo 模型覆盖；不要与 provider_id/model_id 同时传入"),
  },
  async execute(args) {
    return callMarvo("agent-settings", args);
  },
});
