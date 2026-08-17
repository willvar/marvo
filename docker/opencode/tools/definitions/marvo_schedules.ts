import { tool } from "@opencode-ai/plugin";
import { callMarvo } from "../marvo-tool";

const schedule = tool.schema.object({
  kind: tool.schema.enum(["at", "every", "cron", "adaptive"]),
  spec: tool.schema.object({
    at: tool.schema.string().optional().describe("at 的 RFC 3339 未来时间"),
    every_seconds: tool.schema
      .number()
      .int()
      .positive()
      .optional()
      .describe("every 的固定间隔秒数"),
    anchor: tool.schema
      .string()
      .optional()
      .describe("every 的可选 RFC 3339 对齐时间；通常省略"),
    expression: tool.schema
      .string()
      .optional()
      .describe("cron 的五段表达式：分 时 日 月 星期"),
    minimum_seconds: tool.schema
      .number()
      .int()
      .positive()
      .optional()
      .describe("adaptive 的最短检查间隔"),
    maximum_seconds: tool.schema
      .number()
      .int()
      .positive()
      .optional()
      .describe("adaptive 的最长检查间隔"),
    default_seconds: tool.schema
      .number()
      .int()
      .positive()
      .optional()
      .describe("adaptive 未主动调整时的间隔"),
  }),
  timezone: tool.schema
    .string()
    .optional()
    .describe("cron 所需的 IANA 时区，例如 Asia/Hong_Kong"),
});

export default tool({
  description:
    "管理用户的 Marvo 自动任务。at 用于单次未来时间，every 用于固定间隔，cron 用于当地日历时间，adaptive 用于由任务按进展决定下次检查。间隔至少 60 秒。修改前先 list/get 并携带最新 revision；当前自适应任务用 next_check 提议下次检查，已完成的长期任务用 complete 结束。不要向用户展示任务 ID、revision、cron 表达式等内部细节。",
  args: {
    action: tool.schema
      .enum([
        "list",
        "get",
        "create",
        "update",
        "pause",
        "resume",
        "run_now",
        "history",
        "next_check",
        "complete",
        "remove",
      ])
      .describe("要执行的任务操作"),
    id: tool.schema.string().optional().describe("目标自动任务 ID"),
    revision: tool.schema
      .number()
      .int()
      .positive()
      .optional()
      .describe("修改或删除操作所需的当前版本"),
    name: tool.schema
      .string()
      .max(200)
      .optional()
      .describe("用户可读的任务名称"),
    instruction: tool.schema
      .string()
      .max(65536)
      .optional()
      .describe("每次执行时需要完成的完整任务指令"),
    schedule: schedule.optional().describe("create/update 所需的时间安排"),
    next_check_seconds: tool.schema
      .number()
      .int()
      .positive()
      .optional()
      .describe("自适应任务距本轮完成后的下次检查秒数"),
    reason: tool.schema.string().max(1000).optional().describe("暂停原因"),
    limit: tool.schema
      .number()
      .int()
      .min(1)
      .max(100)
      .optional()
      .describe("history 返回条数"),
  },
  async execute(args) {
    return callMarvo("schedules", args);
  },
});
