import os,time,json,signal,resource
os.environ['OMP_NUM_THREADS']='2';os.environ['OPENBLAS_NUM_THREADS']='2';os.environ['MKL_NUM_THREADS']='2';os.sched_setaffinity(0,sorted(os.sched_getaffinity(0))[:2]);signal.alarm(180)
import torch
torch.set_num_threads(2);torch.set_num_interop_threads(1)
from accelerate import Accelerator
from torchmetrics.functional import peak_signal_noise_ratio, mean_squared_error, structural_similarity_index_measure
from models import Model
from data import get_validation_data
from utils import load_checkpoint
from config import Config
start=time.monotonic();model=Model();construct=time.monotonic()-start;start=time.monotonic();load_checkpoint(model,'/tmp/scanner-fsenet-cpu-v060/SD7K.pth');load=time.monotonic()-start;accelerator=Accelerator(cpu=True);model=accelerator.prepare(model);model.eval()
print(json.dumps({'status':'official-model-load-pass','parameters':sum(p.numel() for p in model.parameters()),'construction_seconds':construct,'load_seconds':load,'peak_rss_bytes':resource.getrusage(resource.RUSAGE_SELF).ru_maxrss*1024,'device':str(accelerator.device),'missing_extra_patches':False,'official_model_class':type(model).__module__+'.'+type(model).__name__}))
