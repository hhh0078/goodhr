/** 本文件负责展示微信 Native 支付二维码并轮询云端订单状态。 */
"use client";

import CheckCircleRoundedIcon from "@mui/icons-material/CheckCircleRounded";
import {
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  Typography,
} from "@mui/material";
import { useEffect, useRef, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
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
    title: String(data?.order?.plan_name || fallbackTitle).trim() || fallbackTitle,
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
  const [status, setStatus] = useState<"waiting" | "checking" | "paid" | "error">(
    "waiting",
  );
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
    <Dialog open={Boolean(payment)} onClose={onClose} fullWidth maxWidth='xs'>
      <DialogTitle>微信扫码支付</DialogTitle>
      <DialogContent>
        {payment ? (
          <Stack spacing={2} sx={{ alignItems: "center", textAlign: "center" }}>
            <Box
              sx={{
                p: 2,
                bgcolor: "#ffffff",
                border: "1px solid",
                borderColor: "divider",
                borderRadius: "8px",
                lineHeight: 0,
              }}
            >
              <QRCodeSVG value={payment.codeURL} size={220} level='M' />
            </Box>
            <Box>
              <Typography sx={{ fontWeight: 800 }}>{payment.title}</Typography>
              <Typography sx={{ mt: 0.5, fontSize: 28, fontWeight: 850 }}>
                ￥{payment.amount}
              </Typography>
            </Box>
            <Stack direction='row' spacing={1} sx={{ alignItems: "center" }}>
              {status === "paid" ? (
                <CheckCircleRoundedIcon color='success' />
              ) : (
                <CircularProgress size={18} />
              )}
              <Typography color={status === "error" ? "warning.main" : "text.secondary"}>
                {status === "paid"
                  ? "支付成功，已经到账。"
                  : status === "error"
                    ? "暂时没查到结果，我再悄悄试一次。"
                    : "请使用微信扫码，付完不用点按钮。"}
              </Typography>
            </Stack>
            <Typography sx={{ color: "text.secondary", fontSize: 12 }}>
              订单号：{payment.orderNo}
            </Typography>
          </Stack>
        ) : null}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>稍后再付</Button>
      </DialogActions>
    </Dialog>
  );
}
