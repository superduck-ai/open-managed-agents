import { useEffect, useRef, useState } from 'react';

const examples = {
  zh: [
    '创建一个客服助手，根据我提供的文档回答问题；无法确定答案时，提示用户联系人工确认。',
    '从我粘贴的邮件中提取姓名和金额，只输出 JSON；name 为字符串，amount 为数字，缺失时填 null。',
    '分析我提供的销售表格，汇总各产品的销售额，指出数据缺失和异常，不推测原因。',
    '评审我提供的代码，找出能够确认的问题，说明影响并给出修改建议。',
    '把我提供的会议记录整理成摘要和待办事项，保留负责人和截止日期，未提及的内容标为待确认。',
  ],
  en: [
    'Create a customer support agent that answers from documents I provide and suggests contacting a person when the answer is uncertain.',
    'Extract names and amounts from emails I paste. Return only JSON: name is a string, amount is a number, and missing values are null.',
    'Analyze sales tables I provide, summarize revenue by product, and flag missing data and anomalies without guessing their causes.',
    'Review code I provide, report confirmed issues, explain their impact, and suggest fixes.',
    'Turn meeting notes I provide into a summary and action items. Keep owners and deadlines, and mark unspecified details as needing confirmation.',
  ],
};

export function useBuilderExamples(enabled: boolean, zh: boolean) {
  const [index, setIndex] = useState(0);
  const [focused, setFocused] = useState(false);
  const focus = useRef(false);
  const items = zh ? examples.zh : examples.en;
  useEffect(() => {
    if (!enabled || focused) return;
    const timer = setInterval(() => {
      if (!focus.current && !document.hidden) setIndex((value) => (value + 1) % items.length);
    }, 6000);
    return () => clearInterval(timer);
  }, [enabled, focused, items.length]);
  return {
    example: items[index],
    onFocus: () => {
      focus.current = true;
      setFocused(true);
    },
    onBlur: () => {
      focus.current = false;
      setFocused(false);
    },
  };
}
