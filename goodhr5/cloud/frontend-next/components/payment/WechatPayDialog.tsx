/** 本文件负责展示微信 Native 支付二维码并轮询云端订单状态。 */
"use client";

import { CheckCircleOutlined } from "@ant-design/icons";
import { Button, Flex, Modal, QRCode, Spin, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";

export type WechatPaymentState = {
  orderNo: string;
  title: string;
  amount: string;
  codeURL: string;
};

/** wechatPaymentFromResponse 将云端下单响应转换为二维码弹框数据。 */
export function wechatPaymentFromResponse(
  data: any,
  fallbackTitle: string,
): WechatPaymentState {
  const orderNo = String(data?.order?.order_no || "").trim();
  const codeURL = String(data?.payment?.code_url || "").trim();
  if (!orderNo || !codeURL) {
    throw new Error("微信支付没有返回完整的二维码信息");
  }
  return {
    orderNo,
    codeURL,
    title:
      String(data?.order?.plan_name || fallbackTitle).trim() || fallbackTitle,
    amount: String(data?.order?.amount || "0.00"),
  };
}

/** WechatPayDialog 展示微信支付二维码并在到账后通知父页面刷新。 */
export function WechatPayDialog({
  payment,
  onClose,
  onPaid,
}: {
  payment: WechatPaymentState | null;
  onClose: () => void;
  onPaid: () => void | Promise<void>;
}) {
  const [status, setStatus] = useState<
    "waiting" | "checking" | "paid" | "error"
  >("waiting");
  const onPaidRef = useRef(onPaid);
  onPaidRef.current = onPaid;

  useEffect(() => {
    if (!payment) return;
    let active = true;
    let timer = 0;
    setStatus("waiting");

    /** checkOrder 查询一次订单，未支付时继续等待。 */
    async function checkOrder() {
      if (!active || !payment) return;
      setStatus("checking");
      try {
        const data = await cloudRequest(
          `/api/payment/orders/${encodeURIComponent(payment.orderNo)}`,
        );
        if (!active) return;
        if (data?.order?.status === "paid") {
          setStatus("paid");
          await onPaidRef.current();
          return;
        }
        setStatus("waiting");
      } catch {
        if (active) setStatus("error");
      }
      if (active) timer = window.setTimeout(checkOrder, 2500);
    }

    timer = window.setTimeout(checkOrder, 1500);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [payment]);

  return (
    <Modal
      open={Boolean(payment)}
      onCancel={onClose}
      title="微信扫码支付"
      width={440}
      footer={<Button onClick={onClose}>稍后再付</Button>}
    >
      {payment ? (
        <Flex vertical gap={16} align="center" style={{ textAlign: "center" }}>
          <Flex
            align="center"
            justify="center"
            style={{
              padding: 16,
              background: "#fff",
              border: "1px solid var(--goodhr-border, #dce5e0)",
              borderRadius: 8,
              lineHeight: 0,
            }}
          >
            <QRCode
              type="svg"
              value={payment.codeURL}
              size={220}
              errorLevel="H"
              color="#000000"
              bgColor="#ffffff"
              bordered={false}
              marginSize={0}
              icon="/assets/payment/wechat-pay.png"
              iconSize={{ width: 48, height: 40 }}
              style={{ padding: 0, border: 0 }}
            />
          </Flex>
          <Flex vertical gap={4}>
            <Typography.Paragraph style={{ margin: 0, fontWeight: 800 }}>
              {payment.title}
            </Typography.Paragraph>
            <Typography.Paragraph
              style={{ margin: 0, fontSize: 28, fontWeight: 850 }}
            >
              ￥{payment.amount}
            </Typography.Paragraph>
          </Flex>
          <Flex gap={8} align="center">
            {status === "paid" ? (
              <CheckCircleOutlined
                style={{ color: "#238653", fontSize: 20 }}
                aria-hidden="true"
              />
            ) : (
              <Spin size="small" />
            )}
            <Typography.Paragraph
              style={{
                margin: 0,
                color:
                  status === "error"
                    ? "#c47a1a"
                    : "var(--goodhr-text-secondary, #637069)",
              }}
            >
              {status === "paid"
                ? "支付成功，已经到账。"
                : status === "error"
                  ? "暂时没查到结果，我再悄悄试一次。"
                  : "请使用微信扫码，付完不用点按钮。"}
            </Typography.Paragraph>
          </Flex>
          <Typography.Paragraph
            style={{
              margin: 0,
              color: "var(--goodhr-text-secondary, #637069)",
              fontSize: 12,
            }}
          >
            订单号：{payment.orderNo}
          </Typography.Paragraph>
        </Flex>
      ) : null}
    </Modal>
  );
}
