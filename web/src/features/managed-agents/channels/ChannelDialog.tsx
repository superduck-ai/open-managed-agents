import { QRCodeSVG } from 'qrcode.react';
import { Button } from '../../../shared/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '../../../shared/ui/dialog';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { RadioGroup, RadioGroupItem } from '../../../shared/ui/radio-group';
import { ChannelCredentials, ChannelMode, ChannelOptions } from './ChannelFields';
import { useChannelEditor, type ChannelEditorProps } from './useChannelEditor';

export function ChannelDialog(props: ChannelEditorProps) {
  const { channel, onClose } = props;
  const { value, setValue, binding, setBinding, saved, qr, busy, error, submit } = useChannelEditor(props);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{channel ? '编辑机器人' : '创建机器人'}</DialogTitle>
          <DialogDescription>飞书</DialogDescription>
        </DialogHeader>
        <div className="space-y-6 py-2">
          <div className="space-y-2">
            <Label htmlFor="channel-name">名称 *</Label>
            <Input
              id="channel-name"
              maxLength={40}
              placeholder="最长 40 字"
              value={value.name}
              onChange={(e) => setValue({ ...value, name: e.target.value })}
            />
          </div>
          <ChannelMode value={value.mode} disabled={Boolean(saved)} onChange={(mode) => setValue({ ...value, mode })} />
          {saved?.binding_status !== 'bound' && (
            <div className="space-y-3">
              <Label>绑定方式</Label>
              <RadioGroup
                value={binding}
                aria-label="绑定方式"
                className="flex gap-5"
                onValueChange={(next) => setBinding(String(next))}
              >
                <Label className="flex gap-2">
                  <RadioGroupItem value="qr" />
                  扫码绑定
                </Label>
                <Label className="flex gap-2">
                  <RadioGroupItem value="credentials" />
                  配置凭据
                </Label>
              </RadioGroup>
            </div>
          )}
          {binding === 'credentials' ? (
            <ChannelCredentials value={value} onChange={setValue} bound={saved?.binding_status === 'bound'} />
          ) : (
            <div className="space-y-3">
              <p className="text-xs leading-5 text-muted-foreground">
                生成二维码前会先保存基础配置，请使用飞书 App 扫码授权。
              </p>
              {qr?.status === 'pending' && (
                <div className="flex flex-col items-center gap-3">
                  <div className="bg-white p-4">
                    <QRCodeSVG value={qr.url} size={180} />
                  </div>
                  <p className="text-sm">等待飞书扫码确认</p>
                </div>
              )}
              {qr?.status === 'bound' ? (
                <p role="status" className="text-sm">
                  飞书绑定成功
                </p>
              ) : (
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy || qr?.status === 'pending'}
                  onClick={() => void submit(true)}
                >
                  {qr ? '重新生成二维码' : '生成二维码'}
                </Button>
              )}
              {(qr?.status === 'error' || qr?.status === 'expired') && (
                <p role="alert" className="text-sm text-destructive">
                  二维码已失效，请重新生成。
                </p>
              )}
            </div>
          )}
          <ChannelOptions value={value} onChange={setValue} />
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="grid gap-2">
            <Button disabled={busy} onClick={() => void submit(false)}>
              {busy ? '处理中…' : '保存'}
            </Button>
            <Button variant="outline" disabled={busy} onClick={onClose}>
              取消
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
