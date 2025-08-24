import requests
import threading
import time
import random
import json  # 添加 json 模块用于格式化输出

# 目标URL
url = "http://10.0.168.12:30080/generate"

# 请求头
headers = {
    "Content-Type": "application/json"
}

# 多个不同的提示词，增加多样性
prompts = [
    "请用简单的话解释量子计算",
    "详细描述深度学习的原理和应用",
    "解释Transformer模型在自然语言处理中的作用",
    "讲述人工智能的发展历史",
    "比较机器学习和深度学习的区别",
    "解释神经网络的基本原理",
    "描述计算机视觉的最新进展",
    "讲解自然语言处理中的注意力机制",
    "什么是强化学习？它有哪些应用？",
    "解释生成式对抗网络(GAN)的工作原理"
]

# 发送请求的函数
def send_request(thread_id):
    request_count = 0
    while True:
        try:
            # 随机选择一个提示词
            prompt = random.choice(prompts)

            data = {
                "inputs": f"<|im_start|>system\n你是一个AI助手<|im_end|>\n<|im_start|>user\n{prompt}<|im_end|>\n<|im_start|>assistant\n",
                "parameters": {
                    "max_new_tokens": 512,  # 增加生成的token数量
                    "temperature": 0.7,
                    "top_p": 0.9,
                    "do_sample": True,
                    "repetition_penalty": 1.1
                }
            }

            response = requests.post(url, json=data, headers=headers, timeout=30)
            request_count += 1

            if response.status_code == 200:
                # 获取并打印响应内容
                response_data = response.json()
                generated_text = response_data.get("generated_text", "")

                # 截取部分文本，避免输出过长
                preview_text = generated_text[:100] + "..." if len(generated_text) > 100 else generated_text

                print(f"线程{thread_id} - 请求#{request_count}成功")
                print(f"生成文本: {preview_text}")
                print(f"详细响应: {json.dumps(response_data, indent=2, ensure_ascii=False)[:200]}...")  # 打印部分JSON响应
                print("-" * 80)  # 分隔线
            else:
                print(f"线程{thread_id} - 请求#{request_count}失败，状态码: {response.status_code}")
                print(f"错误响应: {response.text[:200]}")
                print("-" * 80)

        except Exception as e:
            print(f"线程{thread_id} - 请求异常: {e}")
            print("-" * 80)
        
        # 稍微延迟，避免过度请求
        time.sleep(0.1)

# 创建多个线程并发请求
threads = []
for i in range(20):  # 创建20个并发线程
    t = threading.Thread(target=send_request, args=(i,))
    t.daemon = True
    threads.append(t)
    t.start()

# 保持脚本运行
try:
    while True:
        time.sleep(1)
except KeyboardInterrupt:
    print("停止压力测试")